import { randomUUID } from "node:crypto";
import { pickBackend } from "./backends";
import type { AgentEvent } from "./types";

export interface ChatMessage {
  role: "user" | "assistant";
  text: string;
}

export type ChatEvent =
  | { type: "message"; chatId: string; message: ChatMessage }
  | { type: "status"; chatId: string; status: string } // agent run state (running/completed/failed/stopped)
  | { type: "tool"; chatId: string; name: string; input?: unknown } // tool activity (write file, run cmd, …)
  | { type: "done"; chatId: string }
  | { type: "error"; chatId: string; error: string };

/**
 * One 1v1 chat with an agent: message history is cached server-side and every
 * append is broadcast to live subscribers (SSE), so the frontend stays in sync.
 * This is the standard chat-app shape (history server-side, streaming push).
 */
export class ChatSession {
  readonly id = randomUUID();
  readonly agentId: string;
  readonly agentName: string;
  readonly messages: ChatMessage[] = [];
  private listeners = new Set<(e: ChatEvent) => void>();
  private running = false;

  constructor(agentId: string, agentName: string) {
    this.agentId = agentId;
    this.agentName = agentName;
  }

  subscribe(fn: (e: ChatEvent) => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private emit(e: ChatEvent) {
    for (const fn of this.listeners) fn(e);
  }

  /** Append a user message and stream the assistant reply into the history. */
  async post(task: {
    prompt: string;
    runtime: string;
    systemPrompt?: string;
    cwd?: string;
    model?: string;
    provider?: string;
    apiKey?: string;
  }): Promise<void> {
    if (this.running) throw new Error("chat already running");
    this.running = true;
    try {
      this.messages.push({ role: "user", text: task.prompt });
      this.emit({ type: "message", chatId: this.id, message: { role: "user", text: task.prompt } });

      // Build the model context from cached history so the agent "remembers".
      const history = this.messages
        .slice(0, -1) // drop the just-appended user msg; backends get full prompt
        .map((m) => `${m.role === "user" ? "User" : "Assistant"}: ${m.text}`)
        .join("\n");
      const prompt = history ? `${history}\nUser: ${task.prompt}` : task.prompt;

      this.emit({ type: "status", chatId: this.id, status: "running" });
      let reply = "";
      for await (const ev of pickBackend(task.runtime)({
        prompt,
        cwd: task.cwd ?? process.cwd(),
        systemPrompt: task.systemPrompt,
        model: task.model,
        provider: task.provider,
        apiKey: task.apiKey,
      })) {
        if (ev.type === "message") {
          reply += ev.text;
          this.emit({ type: "message", chatId: this.id, message: { role: "assistant", text: ev.text } });
        } else if (ev.type === "tool") {
          // Live activity: e.g. agent writing a file / running a command.
          this.emit({ type: "tool", chatId: this.id, name: ev.name, input: ev.input });
        } else if (ev.type === "session") {
          this.emit({ type: "status", chatId: this.id, status: ev.status });
        } else if (ev.type === "error") {
          this.emit({ type: "error", chatId: this.id, error: ev.message });
          return;
        }
      }
      this.emit({ type: "status", chatId: this.id, status: "completed" });
      if (reply) this.messages.push({ role: "assistant", text: reply });
      this.emit({ type: "done", chatId: this.id });
    } finally {
      this.running = false;
    }
  }
}

/** Registry of cached chat sessions (server-side session cache). */
export class ChatManager {
  private chats = new Map<string, ChatSession>();

  list(): ChatSession[] {
    return [...this.chats.values()];
  }

  get(id: string): ChatSession | undefined {
    return this.chats.get(id);
  }

  create(agentId: string, agentName: string): ChatSession {
    const chat = new ChatSession(agentId, agentName);
    this.chats.set(chat.id, chat);
    return chat;
  }

  remove(id: string): boolean {
    return this.chats.delete(id);
  }
}

// Re-export AgentEvent so consumers only import from here when convenient.
export type { AgentEvent };

import { AgentManager } from "./agents/manager";
import { ChatManager } from "./chat";

/**
 * Local web UI for the daemon: create/configure agents (name, system prompt,
 * runtime cli|llm, model, provider, apiKey, env), then open 1v1 chats with SSE
 * streaming. Session history is cached server-side and pushed live to clients.
 *
 * Endpoints:
 *   GET  /                              → HTML UI
 *   GET  /api/agents                    → list agents
 *   POST /api/agents                    → create agent
 *   DELETE /api/agents/:id              → remove agent
 *   POST /api/agents/:id/chat           → create a chat for an agent
 *   GET  /api/chats                     → list chats (with history)
 *   POST /api/chats/:id/messages        → post a user message (SSE stream)
 *   GET  /api/chats/:id/events          → SSE subscription to a chat
 */
export function startWeb(manager: AgentManager, opts: { port?: number } = {}) {
  const chats = new ChatManager();
  const port = opts.port ?? Number(process.env.DAEMON_WEB_PORT ?? 18081);

  const sse = (stream: ReadableStream<Uint8Array>) =>
    new Response(stream, {
      headers: {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-cache",
        Connection: "keep-alive",
      },
    });

  const json = (v: unknown, status = 200) =>
    new Response(JSON.stringify(v), {
      status,
      headers: { "Content-Type": "application/json" },
    });

  const readBody = async (req: Request): Promise<Record<string, any>> => {
    try {
      return (await req.json()) as Record<string, any>;
    } catch {
      return {};
    }
  };

  const server = Bun.serve({
    port,
    async fetch(req) {
      const url = new URL(req.url);
      const path = url.pathname;

      if (path === "/") {
        return new Response(HTML, { headers: { "Content-Type": "text/html" } });
      }

      // ---- agents ----
      if (path === "/api/agents" && req.method === "GET") {
        return json(manager.list().map((a) => a.snapshot()));
      }
      if (path === "/api/agents" && req.method === "POST") {
        const b = await readBody(req);
        if (!b.name) return json({ error: "name is required" }, 400);
        try {
          const agent = manager.create({
            name: b.name,
            kind: b.kind ?? "executor",
            runtime: b.runtime ?? "mock",
            systemPrompt: b.systemPrompt,
            model: b.model,
            provider: b.provider,
            apiKey: b.apiKey,
            cwd: b.cwd,
          });
          return json(agent.snapshot(), 201);
        } catch (e) {
          return json({ error: (e as Error).message }, 400);
        }
      }
      const agentMatch = path.match(/^\/api\/agents\/([^/]+)$/);
      if (agentMatch && req.method === "DELETE") {
        return json({ ok: manager.remove(agentMatch[1]!) });
      }
      const chatStart = path.match(/^\/api\/agents\/([^/]+)\/chat$/);
      if (chatStart && req.method === "POST") {
        const agent = manager.get(chatStart[1]!);
        if (!agent) return json({ error: "agent not found" }, 404);
        const chat = chats.create(agent.spec.id, agent.spec.name ?? agent.spec.id);
        return json({ chatId: chat.id, agentId: agent.spec.id, name: chat.agentName }, 201);
      }

      // ---- chats ----
      if (path === "/api/chats" && req.method === "GET") {
        return json(chats.list().map((c) => ({ id: c.id, agentId: c.agentId, name: c.agentName, messages: c.messages })));
      }
      const msgMatch = path.match(/^\/api\/chats\/([^/]+)\/messages$/);
      if (msgMatch && req.method === "POST") {
        const chat = chats.get(msgMatch[1]!);
        if (!chat) return json({ error: "chat not found" }, 404);
        const agent = manager.get(chat.agentId);
        if (!agent) return json({ error: "agent not found" }, 404);
        const b = await readBody(req);
        if (!b.text) return json({ error: "text is required" }, 400);

        const stream = new ReadableStream<Uint8Array>({
          start(controller) {
            const encoder = new TextEncoder();
            const send = (ev: string) => controller.enqueue(encoder.encode(`data: ${ev}\n\n`));
            const unsub = chat.subscribe((e) => {
              if (e.type === "message") {
                send(JSON.stringify({ type: "message", role: e.message.role, text: e.message.text }));
              } else if (e.type === "error") {
                send(JSON.stringify({ type: "error", error: e.error }));
              } else if (e.type === "done") {
                send(JSON.stringify({ type: "done" }));
                controller.close();
                unsub();
              }
            });
            void chat
              .post({
                prompt: b.text,
                runtime: agent.spec.runtime,
                systemPrompt: agent.spec.systemPrompt,
                model: agent.spec.model,
                provider: agent.spec.provider,
                apiKey: agent.spec.apiKey,
                cwd: agent.spec.cwd,
              })
              .catch((e) => {
                send(JSON.stringify({ type: "error", error: (e as Error).message }));
                controller.close();
                unsub();
              });
          },
        });
        return sse(stream);
      }
      const evtMatch = path.match(/^\/api\/chats\/([^/]+)\/events$/);
      if (evtMatch && req.method === "GET") {
        const chat = chats.get(evtMatch[1]!);
        if (!chat) return json({ error: "chat not found" }, 404);
        const stream = new ReadableStream<Uint8Array>({
          start(controller) {
            const encoder = new TextEncoder();
            for (const m of chat.messages) {
              controller.enqueue(encoder.encode(`data: ${JSON.stringify({ type: "message", role: m.role, text: m.text })}\n\n`));
            }
            const unsub = chat.subscribe((e) => {
              if (e.type === "message") {
                controller.enqueue(encoder.encode(`data: ${JSON.stringify({ type: "message", role: e.message.role, text: e.message.text })}\n\n`));
              } else if (e.type === "done") {
                controller.enqueue(encoder.encode(`data: ${JSON.stringify({ type: "done" })}\n\n`));
              }
            });
            req.signal.addEventListener("abort", unsub, { once: true });
          },
        });
        return sse(stream);
      }

      return json({ error: "not found" }, 404);
    },
  });

  console.log(`[web] daemon UI on http://127.0.0.1:${server.port}`);
  return server;
}

// Inline single-page UI: create/configure agents, then 1v1 chat with SSE.
const HTML = `<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>chaos.plus daemon</title>
<style>
  body{font-family:system-ui,sans-serif;margin:0;display:flex;height:100vh;background:#0f1115;color:#e6e6e6}
  *{box-sizing:border-box}
  #left{width:320px;border-right:1px solid #2a2d36;padding:16px;display:flex;flex-direction:column;gap:16px;overflow-y:auto}
  #right{flex:1;display:flex;flex-direction:column}
  h1{font-size:16px;margin:0}
  h2{font-size:13px;margin:0 0 8px;color:#9aa0aa;text-transform:uppercase;letter-spacing:.05em}
  input,textarea,select,button{width:100%;margin:4px 0;padding:8px;border:1px solid #2a2d36;border-radius:6px;background:#181b22;color:#e6e6e6;font:inherit}
  textarea{min-height:70px;resize:vertical}
  button{background:#4f6ef7;border:none;cursor:pointer;font-weight:600}
  button:hover{background:#617ef8}
  .agent{display:flex;justify-content:space-between;align-items:center;padding:8px 10px;border:1px solid #2a2d36;border-radius:6px;cursor:pointer;background:#181b22}
  .agent:hover{border-color:#4f6ef7}
  .agent.active{border-color:#4f6ef7;background:#20263a}
  .agent .del{color:#ff5c5c;background:none;border:none;cursor:pointer;padding:0 4px;width:auto}
  #chat{flex:1;overflow-y:auto;padding:24px;display:flex;flex-direction:column;gap:12px}
  .msg{max-width:70%;padding:10px 14px;border-radius:10px;white-space:pre-wrap;line-height:1.5}
  .msg.user{align-self:flex-end;background:#4f6ef7}
  .msg.assistant{align-self:flex-start;background:#242833}
  .msg.err{align-self:flex-start;background:#3a1d1d;color:#ff8a8a}
  #inputbar{display:flex;gap:8px;padding:16px;border-top:1px solid #2a2d36}
  #inputbar input{flex:1;margin:0}
  #inputbar button{width:auto;padding:8px 20px;margin:0}
  .muted{color:#6b7280;font-size:12px}
</style>
</head>
<body>
<div id="left">
  <h1>chaos.plus daemon</h1>
  <section>
    <h2>新建 Agent</h2>
    <input id="f-name" placeholder="名字" />
    <textarea id="f-prompt" placeholder="System prompt / 角色设定"></textarea>
    <select id="f-runtime">
      <option value="claude">CLI · claude</option>
      <option value="codex">CLI · codex</option>
      <option value="mastra">LLM · mastra (API)</option>
      <option value="mock">LLM · mock</option>
    </select>
    <input id="f-model" placeholder="模型（如 claude-sonnet-4-5，可选）" />
    <input id="f-provider" placeholder="provider（如 anthropic / 自定义 baseUrl，可选）" />
    <input id="f-key" placeholder="API Key（LLM 必需；CLI 默认用本地配置）" type="password" />
    <input id="f-env" placeholder="环境变量 JSON（可选，如 {&quot;A&quot;:&quot;b&quot;}）" />
    <button onclick="createAgent()">创建 Agent</button>
  </section>
  <section>
    <h2>Agents</h2>
    <div id="agents"></div>
  </section>
</div>
<div id="right">
  <div id="chatbar" class="muted" style="padding:10px 24px;border-bottom:1px solid #2a2d36">选择或创建一个 Agent 开始会话</div>
  <div id="chat"></div>
  <div id="inputbar" style="display:none">
    <input id="chat-input" placeholder="输入消息…" onkeydown="if(event.key==='Enter')send()" />
    <button onclick="send()">发送</button>
  </div>
</div>
<script>
let agents=[],current=null,currentChat=null,es=null;
const $=id=>document.getElementById(id);
async function api(p,o={}){const r=await fetch(p,{headers:{'Content-Type':'application/json'},...o});return r.json()}
async function refresh(){agents=await api('/api/agents');renderAgents()}
function renderAgents(){const d=$('agents');d.innerHTML=agents.map(a=>\`<div class="agent \${current===a.id?'active':''}" onclick="openAgent('\${a.id}')">\${a.spec.name||a.spec.id}<button class="del" onclick="event.stopPropagation();delAgent('\${a.id}')">×</button></div>\`).join('')}
async function createAgent(){await api('/api/agents',{method:'POST',body:JSON.stringify({name:$('f-name').value,systemPrompt:$('f-prompt').value,runtime:$('f-runtime').value,model:$('f-model').value||undefined,provider:$('f-provider').value||undefined,apiKey:$('f-key').value||undefined})});refresh()}
async function delAgent(id){await api('/api/agents/'+id,{method:'DELETE'});if(current===id){current=null;currentChat=null;$('inputbar').style.display='none';$('chat').innerHTML=''};refresh()}
async function openAgent(id){current=id;const a=agents.find(x=>x.id===id);const chat=await api('/api/agents/'+id+'/chat',{method:'POST'});currentChat=chat.chatId;$('chatbar').textContent='会话：'+(a.spec.name||a.spec.id);$('chat').innerHTML='';$('inputbar').style.display='flex';connectChat(chat.chatId)}
function connectChat(chatId){if(es)es.close();es=new EventSource('/api/chats/'+chatId+'/events');es.onmessage=(e)=>{const d=JSON.parse(e.data);if(d.type==='message')appendMsg(d.role,d.text)}}
async function send(){const t=$('chat-input').value.trim();if(!t)return;$('chat-input').value='';
  const resp=await fetch('/api/chats/'+currentChat+'/messages',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({text:t})});
  if(!resp.ok){appendMsg('err','发送失败: '+resp.status)}
}
function appendMsg(role,text){const el=document.createElement('div');el.className='msg '+role;el.textContent=text;$('chat').appendChild(el);$('chat').scrollTop=$('chat').scrollHeight}
refresh();
</script>
</body>
</html>`;

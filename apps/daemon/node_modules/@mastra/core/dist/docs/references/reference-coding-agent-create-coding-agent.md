> Discover all available pages from the documentation index: https://mastra.ai/llms.txt

# createCodingAgent()

`createCodingAgent()` builds a coding [`Agent`](https://mastra.ai/reference/agents/agent) with portable defaults for the pieces a coding agent always needs: a local workspace, the task-list signal provider, network-retry error processors, and the goal judge prompt. Supply only `model`, `instructions`, and `tools` to get a working agent, or override any default.

The returned value is a standard `Agent`, so it works anywhere an `Agent` does: including as the agent passed to an [`AgentController`](https://mastra.ai/reference/agent-controller/agent-controller-class).

## Usage example

Pass a model, instructions, and tools. The factory fills in the workspace, task signal, error processors, and goal prompt:

```typescript
import { createCodingAgent } from '@mastra/core/coding-agent'

const agent = createCodingAgent({
  id: 'my-coding-agent',
  name: 'My Coding Agent',
  model: 'openai/gpt-5',
  instructions: 'You are a helpful coding assistant.',
  tools: {},
})
```

## Parameters

`createCodingAgent()` accepts every field of [`AgentConfig`](https://mastra.ai/reference/agents/agent) plus the fields below. Fields you provide always take precedence over the factory defaults.

**model** (`MastraLanguageModel | DynamicArgument<MastraLanguageModel>`): The language model the agent uses. Passed straight through to Agent.

**instructions** (`string | DynamicArgument<string>`): System instructions for the agent. Passed straight through to Agent.

**tools** (`ToolsInput | DynamicArgument<ToolsInput>`): Tools available to the agent. Passed straight through to Agent.

**workspace** (`AnyWorkspace | undefined`): The workspace backing the agent. When the key is omitted, a default local workspace is built. When set explicitly to undefined, the factory builds no default — opt out when the workspace is wired elsewhere (for example at the AgentController level).

**basePath** (`string`): Base path for the default workspace built when workspace is omitted. (Default: `process.cwd()`)

**signals** (`SignalProvider[]`): Signal providers for the agent. When omitted, defaults to a single TaskSignalProvider.

**errorProcessors** (`Processor[]`): Error processors for the agent. When omitted, defaults to unknown stream-error retries with specialized ECONNRESET and bad-request policies, plus PrefillErrorHandler and ProviderHistoryCompat.

**goal** (`AgentGoalConfig`): Goal configuration. When provided without a prompt, the prompt defaults to DEFAULT\_GOAL\_JUDGE\_PROMPT.

## Returns

**agent** (`Agent`): A coding agent with the resolved workspace, signals, error processors, and goal applied.

## Defaults

The factory only fills a default when you don't provide the corresponding field. Caller-provided values always win.

| Field             | Default when omitted                                                                                                                           |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `workspace`       | A [`Workspace`](https://mastra.ai/reference/workspace/workspace-class) backed by `LocalFilesystem` and `LocalSandbox` rooted at the base path. |
| `signals`         | A single [`TaskSignalProvider`](https://mastra.ai/reference/signals/task-signal-provider).                                                     |
| `errorProcessors` | Unknown stream-error retries with specialized ECONNRESET and bad-request policies, plus `PrefillErrorHandler` and `ProviderHistoryCompat`.     |
| `goal.prompt`     | `DEFAULT_GOAL_JUDGE_PROMPT` (only when a `goal` is configured).                                                                                |

### Workspace

When the `workspace` key is omitted, the factory builds a local workspace rooted at `basePath` (default `process.cwd()`):

```typescript
import { Workspace, LocalFilesystem, LocalSandbox } from '@mastra/core/workspace'

new Workspace({
  filesystem: new LocalFilesystem({ basePath }),
  sandbox: new LocalSandbox({ workingDirectory: basePath }),
})
```

To opt out (for example, when the workspace is injected at the [`AgentController`](https://mastra.ai/reference/agent-controller/agent-controller-class) level), pass `workspace: undefined` explicitly:

```typescript
const agent = createCodingAgent({
  id: 'my-coding-agent',
  name: 'My Coding Agent',
  model: 'openai/gpt-5',
  instructions: 'You are a helpful coding assistant.',
  tools: {},
  workspace: undefined, // opt out of the default workspace
})
```

### Error processors

The default [`StreamErrorRetryProcessor`](https://mastra.ai/reference/processors/stream-error-retry-processor) applies these retry policies:

- Unknown errors left unmatched by provider metadata or a specific matcher retry up to twice with a `3000ms` delay. Known authorization failures surface immediately.
- Network resets (`ECONNRESET` / `socket hang up`) retry up to twice with exponential backoff (`1000ms * 2^retryCount`, capped at `30000ms`).
- Bad-request errors retry once after `2000ms`.

Specific network-reset and bad-request policies take precedence over the unknown-error policy. Passing `errorProcessors` replaces the default processor stack. `PrefillErrorHandler` and `ProviderHistoryCompat` are also included for provider compatibility.

## Related

- [`buildBasePrompt()`](https://mastra.ai/reference/coding-agent/build-base-prompt)
- [`Agent`](https://mastra.ai/reference/agents/agent)
- [`AgentController`](https://mastra.ai/reference/agent-controller/agent-controller-class)
> Discover all available pages from the documentation index: https://mastra.ai/llms.txt

# AgentController

> **Beta:** The `AgentController` feature is in beta stage and subject to breaking changes in minor versions until it graduates from its beta status.

The `AgentController` class is a shared host for one or more [`Session`](https://mastra.ai/reference/agent-controller/session) instances. Initialize the controller, create a session, then use `session.*` APIs for conversation state and run control.

For a guided introduction, see the [AgentController overview](https://mastra.ai/docs/harness/agent-controller).

## Usage example

The following example initializes a controller and creates a session. It subscribes to session events before sending a message.

```typescript
import { Agent } from '@mastra/core/agent'
import { AgentController } from '@mastra/core/agent-controller'
import { Workspace } from '@mastra/core/workspace'

const agent = new Agent({
  id: 'coding-agent',
  name: 'Coding agent',
  instructions: 'Help with software engineering tasks.',
  model: 'anthropic/claude-sonnet-4-6',
})

const controller = new AgentController({
  id: 'coding-controller',
  agent,
  workspace: new Workspace({ id: 'coding-workspace' }),
  modes: [{ id: 'build', name: 'Build', metadata: { default: true } }],
})

await controller.init()

const session = await controller.createSession({ resourceId: 'project-42' })
const unsubscribe = session.subscribe(event => {
  if (event.type === 'message_update') {
    console.log(event.message)
  }
})

await session.sendMessage({ content: 'Review the project structure.' })
unsubscribe()
```

## Constructor parameters

**id** (`string`): Unique controller identifier. It is also the default session and resource identifier.

**modes** (`AgentControllerMode[]`): Mode definitions available to every session. At least one mode is required.

**modes.id** (`string`): Unique mode identifier.

**modes.name** (`string`): Display name.

**modes.defaultModelId** (`string`): Model selected when a session enters this mode without a stored selection.

**modes.description** (`string`): Text shown in mode selectors.

**modes.instructions** (`string`): Instructions layered above the backing agent instructions for this mode.

**modes.transitionsTo** (`string`): Mode entered after an approved submit\_plan suspension.

**modes.availableTools** (`string[]`): Allowlist of exposed tool names. An empty array hides every tool in this mode.

**modes.metadata** (`Record<string, unknown>`): Pass-through mode metadata. metadata.default: true marks the default mode.

**modes.tools** (`ToolsInput`): Mode tools. Mutually exclusive with additionalTools.

**modes.additionalTools** (`ToolsInput`): Tools added to the backing agent tools. Mutually exclusive with tools.

**modes.agent** (`Agent`): Deprecated mode-specific agent. Use the top-level agent parameter.

**modes.default** (`boolean`): Deprecated default marker. Use metadata.default or defaultModeId.

**agent** (`Agent`): Shared backing agent used by the configured modes.

**resourceId** (`string`): Default resource identifier for sessions and threads. Defaults to id.

**storage** (`MastraCompositeStore`): Storage used for persistent threads, messages, settings, and resumable run data.

**stateSchema** (`PublicSchema<TState, any>`): Schema used to validate session.state updates.

**initialState** (`Partial<TState>`): Initial state merged with schema defaults for each new session.

**memory** (`DynamicArgument<MastraMemory>`): Memory instance shared with backing agents that do not define their own memory.

**defaultModeId** (`string`): Default mode identifier. It takes precedence over mode metadata.

**instructions** (`string`): Controller instructions layered with the current mode instructions.

**tools** (`DynamicArgument<ToolsInput | undefined>`): Tools shared by controller runs and available to configured subagents.

**workspace** (`DynamicArgument<Workspace | undefined>`): Static workspace or per-session workspace factory. A session must resolve a valid workspace.

**browser** (`DynamicArgument<MastraBrowser | undefined>`): Static browser or per-session browser factory.

**channels** (`AgentControllerChannelsConfig`): Chat channel configuration used to route channel threads into controller sessions.

**intervalHandlers** (`IntervalHandler[]`): Periodic handlers started by init() and stopped by stopIntervals() or destroy().

**idGenerator** (`() => string`): Custom identifier generator for threads, messages, and signals.

**modelUseCountProvider** (`ModelUseCountProvider`): Returns model usage counts used to sort available models.

**modelUseCountTracker** (`ModelUseCountTracker`): Records a model selection after session.model.switch().

**subagents** (`AgentControllerSubagent[]`): Subagent types exposed through the built-in subagent tool.

**subagents.id** (`string`): Unique subagent type identifier.

**subagents.name** (`string`): Display name.

**subagents.description** (`string`): Description used by the generated tool.

**subagents.instructions** (`DynamicArgument<AgentInstructions>`): Subagent instructions.

**subagents.tools** (`ToolsInput`): Tools owned by the subagent.

**subagents.allowedControllerTools** (`string[]`): Controller tool IDs added to the subagent tools.

**subagents.allowedWorkspaceTools** (`string[]`): Workspace tool names visible to the subagent.

**subagents.defaultModelId** (`string`): Default subagent model.

**subagents.maxSteps** (`number`): Maximum execution steps.

**subagents.stopWhen** (`LoopOptions["stopWhen"]`): Loop stop condition.

**subagents.forked** (`boolean`): Whether the subagent inherits a cloned parent thread by default.

**gateways** (`MastraModelGatewayInterface[]`): Custom model gateways merged with the built-in gateways.

**omConfig** (`AgentControllerOMConfig`): Default observational memory models and thresholds.

**disableBuiltinTools** (`BuiltinToolId[]`): Built-in controller tools to omit from runs.

**toolCategoryResolver** (`(toolName: string) => ToolCategory | null`): Maps tool names to permission categories.

**pubsub** (`PubSub`): PubSub implementation propagated to backing agents.

**threadLock** (`{ acquire: (threadId: string) => void | Promise<void>; release: (threadId: string) => void | Promise<void> }`): Lock implementation used to coordinate thread ownership.

**observability** (`ObservabilityEntrypoint`): Observability configuration for a standalone controller Mastra instance.

## Properties

**id** (`string`): The controller identifier passed to the constructor.

## Methods

### Sessions

#### `createSession(options)`

Get or create the live session registered for the `(resourceId, scope)` pair. Call `init()` before this method.

```typescript
const session = await controller.createSession({
  resourceId: 'project-42',
  scope: 'editor-window-1',
  threadId: 'thread-7',
})
```

The same `resourceId` and `scope` return the same `Session` instance. A different scope creates an isolated session for the same resource. When `threadId` is supplied, the method switches a cached session to that thread or creates the thread when it doesn't exist.

**resourceId** (`string`): Memory resource and live-session registry key. Defaults to the configured resourceId or controller id.

**scope** (`string`): Optional registry namespace that allows multiple live sessions for one resource.

**threadId** (`string`): Exact thread to bind. Missing threads are created with this identifier.

**id** (`string`): Stable session identifier. Defaults to the controller id.

**ownerId** (`string`): Stable session owner identifier. Defaults to id.

**tags** (`Record<string, string>`): Tags copied to threads created by the session.

**workspace** (`Workspace`): Workspace override for this session.

**browser** (`MastraBrowser`): Browser override for this session.

**requestContext** (`RequestContext`): Context used to resolve dynamic workspace and browser factories.

Returns: `Promise<Session<TState>>`

#### `getSessionByResource(resourceId, scope?)`

Return the live session registered for a resource and optional scope.

```typescript
const session = await controller.getSessionByResource('project-42', 'editor-window-1')
```

Returns: `Promise<Session<TState> | undefined>`

#### `setResourceId(session, { resourceId })`

Move a live session to another resource and clear its active thread binding.

```typescript
await controller.setResourceId(session, { resourceId: 'project-43' })
```

#### `getKnownResourceIds(session)`

List resource identifiers present in stored threads.

```typescript
const resourceIds = await controller.getKnownResourceIds(session)
```

Returns: `Promise<string[]>`

### Lifecycle

#### `init()`

Initialize shared storage, workspace services, and configured interval handlers. Repeated calls reuse the same initialization promise.

```typescript
await controller.init()
```

#### `destroy()`

Stop controller-owned interval handlers. This doesn't destroy Sessions created by the controller.

```typescript
await controller.destroy()
```

### Modes and agents

#### `listModes()`

Return the configured mode definitions.

```typescript
const modes = controller.listModes()
```

Returns: `AgentControllerMode[]`

#### `getCurrentAgent(session)`

Return the backing agent for the session's active mode.

```typescript
const agent = controller.getCurrentAgent(session)
```

Returns: `Agent`

### Workspace and browser

#### `hasWorkspace()`

Report whether the controller has a static, dynamic, or object-based workspace configuration.

```typescript
if (controller.hasWorkspace()) {
  console.log('Workspace configured')
}
```

Returns: `boolean`

#### `isWorkspaceReady()`

Report whether the controller-level workspace is ready.

```typescript
const ready = controller.isWorkspaceReady()
```

Returns: `boolean`

#### `getWorkspace()`

Return a static controller workspace. Dynamic workspace factories return `undefined` until resolved.

```typescript
const workspace = controller.getWorkspace()
```

Returns: `Workspace | undefined`

#### `resolveWorkspace({ session, requestContext? })`

Resolve a dynamic workspace for a session and cache the result on the controller.

```typescript
const workspace = await controller.resolveWorkspace({ session, requestContext })
```

Returns: `Promise<Workspace | undefined>`

#### `setBrowser(browser)`

Replace the controller browser and propagate it to the backing agents.

```typescript
controller.setBrowser(browser)
```

### Mastra and channels

#### `getMastra()`

Return the parent Mastra instance or the internal instance created by `init()`.

```typescript
const mastra = controller.getMastra()
```

Returns: `Mastra | undefined`

#### `getChannels()`

Return the configured chat channel integration.

```typescript
const channels = controller.getChannels()
```

Returns: `AgentControllerChannels | null`

### Models

#### `getCurrentModelAuthStatus(session)`

Return authentication status for the session's selected model.

```typescript
const status = await controller.getCurrentModelAuthStatus(session)
```

Returns: `Promise<ModelAuthStatus>`

#### `listAvailableModels()`

List models from the configured and built-in gateways. Results are cached briefly and sorted with usage data when `modelUseCountProvider` is configured.

```typescript
const models = await controller.listAvailableModels()
```

Returns: `Promise<AvailableModel[]>`

#### `invalidateAvailableModelsCache()`

Clear the available-model cache.

```typescript
controller.invalidateAvailableModelsCache()
```

### Observational memory and permissions

#### `loadOMProgress(session)`

Load stored observational memory progress for the active thread and emit an `om_status` event.

```typescript
await controller.loadOMProgress(session)
```

#### `getObservationalMemoryRecord(session)`

Return the observational memory record for the active thread.

```typescript
const record = await controller.getObservationalMemoryRecord(session)
```

Returns: `Promise<ObservationalMemoryRecord | null>`

#### `getToolCategory({ toolName })`

Resolve the permission category for a tool.

```typescript
const category = controller.getToolCategory({ toolName: 'execute_command' })
```

Returns: `ToolCategory | null`

### Intervals

#### `registerInterval(handler)`

Start or replace a periodic handler.

```typescript
controller.registerInterval({
  id: 'refresh',
  intervalMs: 60_000,
  handler: async () => refreshData(),
})
```

#### `removeInterval({ id })`

Stop one interval and run its optional shutdown callback.

```typescript
await controller.removeInterval({ id: 'refresh' })
```

#### `stopIntervals()`

Stop all intervals and run their optional shutdown callbacks.

```typescript
await controller.stopIntervals()
```

## Related

- [AgentController guide](https://mastra.ai/docs/harness/agent-controller)
- [Session reference](https://mastra.ai/reference/agent-controller/session)
- [Agents](https://mastra.ai/docs/agents/overview)
- [Workspace](https://mastra.ai/docs/workspace/overview)
- [Channels](https://mastra.ai/docs/capabilities/channels/overview)
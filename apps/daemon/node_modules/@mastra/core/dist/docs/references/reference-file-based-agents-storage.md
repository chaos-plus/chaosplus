> Discover all available pages from the documentation index: https://mastra.ai/llms.txt

# Storage

Mastra sets the project's default [storage](https://mastra.ai/docs/storage/overview) from a `storage.ts` file directly under `src/mastra/`. The file default-exports a store, which replaces the built-in in-memory store used for memory, workflows, observability, and other storage domains.

Use this page for the file-based convention. For backend choice, storage domains, retention, and provider details, see [storage overview](https://mastra.ai/docs/storage/overview).

## Quickstart

Use [`LibSQLStore`](https://mastra.ai/reference/storage/libsql) for a local file-backed store:

```typescript
import { LibSQLStore } from '@mastra/libsql'

export default new LibSQLStore({
  id: 'mastra-storage',
  url: 'file:./mastra.db',
})
```

Mastra registers the store before file-based agents and workflows, so storage-dependent primitives bind to this store instead of the default in-memory store.

## Production backends

`storage.ts` can export any Mastra storage adapter, such as LibSQL, PostgreSQL, or MongoDB. For setup patterns, provider support, and schema details, see [storage overview](https://mastra.ai/docs/storage/overview), [observability signal support](https://mastra.ai/docs/observability/overview), and the [storage reference](https://mastra.ai/reference/storage/overview).

## Precedence with code

Code-registered storage wins over `storage.ts`. Use `storage.ts` when one project-wide store is enough. Use code registration when setup depends on runtime wiring in `src/mastra/index.ts`.
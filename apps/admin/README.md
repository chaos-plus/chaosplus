# Chaosplus IAM Administration

Bun and Turborepo workspace for the Chaosplus IAM operator console.

```powershell
bun install
bun run dev
bun run lint
bun run typecheck
bun run test
bun run build
```

The application runs from `apps/web`; reusable primitives live in `packages/ui`. Browser API calls use `/api` with Cookie credentials. Vite forwards that prefix to `VITE_API_PROXY_TARGET`; the production Nginx image forwards it to the `chaosplus` service.

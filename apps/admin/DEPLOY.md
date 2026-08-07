# Deploying The Chaosplus Admin Console

The root `Dockerfile` builds the Bun workspace and serves `apps/web/dist` with Nginx on port `8080`.

Build from `apps/admin`:

```powershell
docker build -t chaosplus-admin:local .
```

The container expects the backend at `http://chaosplus:8080` on its Docker network. Nginx serves the SPA, proxies `/api/*` without the `/api` prefix, and exposes `GET /healthz`.

`VITE_API_URL` is a public build value and defaults to `/api`. Never place credentials or private service addresses in a `VITE_*` variable.

# Chaosplus Documentation

Astro and Starlight publication site for the Chaosplus engineering documentation.

Root `README.md` and `docs/*.md` are authoritative. `bun run sync` generates the corresponding pages under `src/content/docs`; do not edit generated pages directly. The production build parses every generated HTML page, resolves local links and assets, verifies Mermaid output, and checks the search-index policy before optional encryption.

```powershell
bun install
bun run dev
bun run check
bun run build
```

The development server defaults to `http://localhost:4321`. Set `DOC_SITE_URL` to the public canonical URL when a sitemap is required. Set `DOC_PASSWORD` only when a client-side encrypted static build is required; protected builds never publish a sitemap. Client-side protection does not replace server-side authentication.

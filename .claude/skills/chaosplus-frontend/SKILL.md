---
name: chaosplus-frontend
description: Develop, review, test, optimize, and deploy the Chaosplus IAM administration frontend. Use for changes under web/admin, React or TypeScript code, the Bun/Turborepo workspace, shared UI components, API clients, browser authentication, responsive behavior, accessibility, frontend tests, Nginx, or frontend containers.
---

# Chaosplus Frontend

Build a quiet, efficient IAM administration application on the repository's Bun, Turborepo, React, Vite, and shared UI stack.

## Start With Repository Truth

1. Run `python .agents/skills/chaosplus-quality-gate/scripts/skill-runtime.py refresh` from the repository root.
2. Read `../chaosplus-quality-gate/references/repository-facts.md` and `references/lessons.md`.
3. Inspect `web/admin/package.json`, the affected app/package manifest, routing, API client, and nearby components.
4. Confirm the backend operation in `/openapi.json` or backend source before coding against it.

The copied SevenLink project supplies framework and design-system structure, not mall business contracts. Remove copied assumptions instead of adapting IAM around them.

## Preserve Workspace Boundaries

- Keep the deployable application in `web/admin/apps/web`.
- Keep reusable primitives and design tokens in `web/admin/packages/ui`; do not couple that package to IAM routes or API payloads.
- Keep page composition in `apps/web/src/app`, reusable app components in `src/components`, and the typed backend client in `src/lib`.
- Use existing `@workspace/ui` and Lucide components before adding dependencies.
- Keep one API request implementation. Route development and production browser calls through `/api`, with credentials included and the prefix stripped by the proxy.
- Do not place secrets in `VITE_*`; browser configuration is public.

## Implement Complete Operator Workflows

- Preserve Cookie session authentication, protected routes, return URL validation, anonymous/loading/error states, and explicit logout.
- Make tenant context visible and stable. Future entity selection belongs beneath tenant context and must not imply cross-tenant access.
- Represent mutations with pending, success, validation, authorization, empty, and retry states.
- Use dense tables and forms suited to repeated IAM operations; avoid decorative landing-page composition.
- Use icon buttons for familiar actions, labels for ambiguous actions, visible focus, semantic HTML, associated form labels, and 44px touch targets where practical.
- Keep text within controls at desktop and mobile widths. Reserve fixed dimensions for navigation, toolbars, icon buttons, and tables so loading state does not shift layout.
- Avoid nested cards, excessive rounding, one-hue screens, and explanatory feature prose in the application.

## Test Real Behavior

- Use real `Bun.serve` TCP listeners for API-client tests.
- Use a real backend and browser for authentication, route, Cookie, and mutation workflows.
- Do not use mocks, fakes, stubs, intercepted fixtures, or test-only application branches.
- Test failure envelopes and non-2xx statuses, not only successful JSON.
- Inspect desktop and mobile screenshots for overflow, overlap, unreadable text, empty rendering, and focus behavior.
- Treat lint warnings as failures and review production bundle size after build.

## Verify And Learn

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .agents/skills/chaosplus-quality-gate/scripts/check-gates.ps1 -Scope frontend
```

The gate runs lint, typecheck, real tests, production build, forbidden-test-substitute scans, and deployment-file checks. Use `-Full` before release for browser/runtime checks configured by the repository.

After fixing a verified recurring failure, append an evidence-backed lesson:

```text
python .agents/skills/chaosplus-quality-gate/scripts/skill-runtime.py record --domain frontend --symptom "..." --cause "..." --prevention "..." --evidence "test, screenshot, or build output"
```

Do not teach the skill visual preferences from a single page or weaken checks to preserve copied code.

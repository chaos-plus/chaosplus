# Chaosplus Agent Rules

Use `.agents/skills/chaosplus-quality-gate` for every repository implementation, review, architecture, test, release, deployment, or documentation task. Run its context refresh before making architectural assumptions.

Route work by path:

- `cmd`, `internal`, `pkg`, Go manifests, backend database or runtime changes: use `chaosplus-backend`.
- `web/admin` and its container or browser proxy: use `chaosplus-frontend`.
- `web/docs`, `docs`, and `README.md`: use `chaosplus-docs`.
- `.agents`, `.github`, this file, or cross-domain work: keep `chaosplus-quality-gate` active and add every affected domain skill.

Root `README.md`, `docs/*.md`, production code, migrations, manifests, generated OpenAPI, and passing real tests are the sources of truth. Copied SevenLink projects provide structure only and do not define Chaosplus business behavior.

Skills may autonomously refresh generated repository facts and append evidence-backed lessons. They must not autonomously change security invariants, architecture decisions, dependency policy, quality scripts, exclusions, or thresholds. Never weaken a gate to make a task pass.

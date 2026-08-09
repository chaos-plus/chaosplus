---
name: dev-docs
description: Develop, reorganize, validate, and publish the Dev documentation site and architecture documentation. Use for changes under docs or docs, README documentation, Astro/Starlight configuration, Mermaid diagrams, API and deployment guides, navigation, document synchronization, link checks, or documentation containers.
---

# Dev Docs

Maintain an understandable, executable documentation system: readers must be able to find the contract, understand the architecture, and identify where code belongs.

## Establish Sources Of Truth

1. Run `python .claude/skills/dev-quality-gate/scripts/skill-runtime.py refresh` from the repository root.
2. Read `../dev-quality-gate/references/repository-facts.md` and `references/lessons.md`.
3. Treat root `README.md` and `apps/docs/*.md` as authoritative engineering documents.
4. Treat `apps/docs` as the Astro/Starlight publication layer. Use its sync script for mirrored source pages; do not hand-edit generated copies.
5. Verify claims in production code, tests, configuration structs, migrations, and OpenAPI output.

Do not retain SevenLink product names, mall-specific assumptions, Figma ledgers, routes, or business rules merely because the site engine was copied from that project.

## Write Implementation-Grade Documents

- State purpose, scope, non-goals, and current implementation status.
- Show module ownership and dependency direction before file-level details.
- Define domain terms and distinguish principal, credential, session, tenant, membership, entity, role, permission, policy, and business resource.
- Include request flows or Mermaid diagrams when three or more components interact.
- Specify data ownership, transaction boundaries, authorization checks, error behavior, configuration, migrations, and observability.
- Include concrete repository paths, interface shapes, API examples, and test locations so a developer can begin implementation without guessing.
- Separate implemented behavior from target architecture. Never describe a roadmap item as complete.
- Keep secrets and real DSNs out of examples. Use obviously synthetic values.
- Keep headings stable and links relative where possible.

## Maintain The Publication Site

- Keep navigation concise and task-oriented: overview, architecture, backend, IAM, operations, quality.
- Keep the first page as documentation, not a marketing landing page.
- Preserve Starlight search, accessible navigation, Mermaid rendering, responsive layout, and production build output.
- Prefer deterministic scripts using structured parsers over manual duplicated indexes.
- Write generated files only when content changes to avoid noisy diffs.
- Remove checks that only validate copied SevenLink artifacts; retain or replace them with Dev-relevant build, link, structure, and protected-build checks.

## Verify And Learn

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .claude/skills/dev-quality-gate/scripts/check-gates.ps1 -Scope docs
```

The gate synchronizes authoritative documents, validates internal links and navigation, renders Mermaid, and builds the Astro site. Inspect the built home page and at least one long architecture page at desktop and mobile widths when layout changes.

Record only evidence-backed recurring documentation failures:

```text
python .claude/skills/dev-quality-gate/scripts/skill-runtime.py record --domain docs --symptom "..." --cause "..." --prevention "..." --evidence "build or link-check output"
```

Do not record changing implementation facts as lessons; `skill-runtime.py refresh` owns those facts.

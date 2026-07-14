# Module structure (DDD-lite)

How modules are organized in this codebase. The goal is a consistent, DDD-flavored
layout that **scales with the module's size** — small modules stay flat, larger
ones split into sub-packages — without ceremony that doesn't earn its keep.

## Principles

1. **DDD naming, always.** The concepts `domain`, `service`, `repository`, `api`
   are named consistently whether they are a single file (`service.go`) or a
   package (`service/`). A reader can always tell which layer a piece of code is.
2. **File → directory by size.** A layer is a **file** while it is one cohesive
   unit; it is promoted to a **package (directory)** when it grows (multiple
   entities/use-cases, or a file pushing past ~400 lines). Idiomatic Go: start
   with a file, split to a package when the pressure is real.
3. **Apply only the layers a module needs (YAGNI).** Infrastructure modules have
   no domain entities — do not give them empty `domain/`/`repository/` folders.
4. **Dependency direction:** `api → service → domain ← repository`. The domain is
   pure (no transport, no infra). The module root wires the layers together.
5. **Acyclic layering (important).** The module **root package** holds `module.go`
   (composition + lifecycle) and, for infra modules, the core logic. A transport
   sub-package (`api/`) must **not import the module root** — that would create an
   import cycle (`root → api → root`). Instead, `api/` defines a small **port**
   (an interface or function type) and the module **injects** the implementation.
   `api/` depends only on the port, generated proto, and shared extensions
   (`respx`, …). This keeps the graph acyclic and the transport layer testable in
   isolation.

## Placement

- Domain modules: `internal/modules/<module>/`.
- Infrastructure modules (id generation, geoip, locks, …): `internal/infra/<module>/`.

## Layouts by size

### A. Infrastructure module (no domain)

```
internal/infra/guid/
  module.go              # composition + lifecycle; implements app capabilities
  guid.go  id.go         # core logic
  api/                   # transport; depends on an injected port, not the root
    rest.go  grpc.go
  proto/                 # per-module proto (buf); sources and generated code split
    api/v1/guid.proto            # source
    gen/go/api/v1/*.pb.go        # generated; gen mirrors the source path
  i18n/locales/*.json
```

### B. Domain module, small — DDD as files (one package)

```
internal/modules/order/
  module.go
  domain.go              # entities, value objects, domain errors, Repository port
  service.go             # application/use-case logic
  repository.go          # bun implementation of the domain Repository port
  api/  rest.go  grpc.go
  proto/  api/v1/*.proto  gen/go/...
  sql/{sqlite,mysql,postgres}/*.sql   # goose migrations (goosex convention)
  i18n/locales/*.json
```

### C. Domain module, grown — DDD as directories (sub-packages)

```
internal/modules/order/
  module.go
  domain/       entity.go  value_object.go  errors.go  repository.go   # package domain (pure)
  service/      *.go        # depends on domain
  repository/   *.go        # implements domain ports; depends on bun
  api/  rest.go  grpc.go
  proto/v1/  sql/{...}  i18n/
```

**Promotion trigger (B → C):** when a layer gains multiple entities/use-cases or a
file grows past ~400 lines, promote `x.go` to a package `x/`. This changes import
paths once — a worthwhile, one-time cost.

## Transport (`api/`)

- Holds `rest.go` (huma handlers) and `grpc.go` (gRPC server).
- Receives a **port** from `module.go` (e.g. `type NextFunc func() (int64, error)`),
  never importing the module root. See `internal/infra/guid/api` for the reference.
- `module.go` implements the app's `RESTRegistrar` / `GRPCRegistrar` by delegating
  to `api.RegisterREST` / `api.RegisterGRPC` with the injected port.

## Protobuf & gRPC (buf)

Managed with [buf](https://buf.build). **`go_package` is the single source of truth**
for both the import path and the on-disk location of generated code.

- **Per-module proto**: sources under `proto/api/v1/*.proto`; generated Go under
  `proto/gen/go/api/v1/` (gen mirrors the source path) — the two are kept separate.
- **Descriptive, versioned package** `chaosplus.<mod>.v1` (not a shared `api.v1`),
  so gRPC services get unique names like `chaosplus.guid.v1.GuidService`.
- Each `.proto` declares its full `go_package`, e.g.
  `github.com/chaos-plus/chaosplus/internal/infra/guid/proto/gen/go/api/v1;guidv1`.
- `buf.gen.yaml` runs with `managed.enabled: false` and
  `opt: module=github.com/chaos-plus/chaosplus`, which strips the module prefix
  from `go_package` and writes the remainder under `out: .` — so files land exactly
  where `go_package` says.
- Two lists to keep in sync as modules are added: `buf.yaml` `modules:` (workspace
  roots, for cross-module `import "api/v1/<name>.proto"` and lint) and
  `buf.gen.yaml` `inputs:` (what to generate).
- Regenerate after editing a `.proto`:

  ```bash
  buf lint && buf generate
  ```

- Generated `*.pb.go` are committed so `go build` works without buf installed.

## Checklist for a new module

- [ ] `module.go` implements the capabilities it needs (Migrator/Starter/Stopper/
      RESTRegistrar/GRPCRegistrar) and is wired in `internal/app/modules.go`.
- [ ] Transport in `api/`, depending on an injected port (no import cycle).
- [ ] proto (if any): sources at `proto/api/v1/`, listed in both `buf.yaml`
      `modules:` and `buf.gen.yaml` `inputs:`; `go_package` points to `proto/gen/go/…`.
- [ ] Migrations (if any) under `sql/<dialect>/`.
- [ ] i18n keys (if any) under `i18n/locales/`.
- [ ] Domain layers present only if the module has a domain; files first, packages
      when they grow.

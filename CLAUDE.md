# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**dynupdate** is a CoreDNS plugin that enables authenticated, dynamic DNS record management at runtime via REST API and gRPC. Records are served from a thread-safe in-memory store with atomic JSON persistence and optional auto-reload.

- **Go module**: `github.com/mauromedda/coredns-updater-plugin`
- **Go version**: 1.25.6
- **Package name**: `dynupdate` (all source files in the root)

## Build & Development Commands

```bash
make build            # Compile check (go build ./...)
make test             # Run all tests with -race
make test-cover       # Tests + coverage report
make test-cover-html  # HTML coverage visualization
make lint             # golangci-lint run ./...
make vet              # go vet ./...
make fmt              # gofmt + goimports
make check            # Full quality gate: vet + lint + test
make tidy             # go mod tidy
make clean            # Remove coverage artifacts, clear test cache
make proto            # Regenerate Go code from proto/dynupdate.proto
make proto-lint       # Lint proto files
```

Run a single test:
```bash
go test -v -race -run TestFunctionName ./...
```

## Architecture

The plugin follows the standard CoreDNS plugin lifecycle:

```
init() → plugin.Register("dynupdate", setup)

setup(caddy.Controller)
  ├─ parseConfig()      → pluginConfig from Corefile
  ├─ NewStore()         → in-memory store + JSON persistence
  ├─ NewAPIServer()     → REST endpoint (HTTP)
  ├─ NewGRPCServer()    → gRPC endpoint
  ├─ OnStartup()        → start API + gRPC listeners
  ├─ OnShutdown()       → graceful stop, persist store
  └─ AddPlugin()        → register DNS handler in chain
```

### Key Components

| File | Responsibility |
|------|---------------|
| `setup.go` | Corefile parsing, `pluginConfig`, plugin registration, lifecycle hooks |
| `store.go` | Thread-safe `Store` (map[string][]Record), atomic file I/O, auto-reload, `SyncPolicy` enforcement |
| `dynupdate.go` | `DynUpdate` (plugin.Handler): serves DNS queries, CNAME chasing (max 10 hops), zone-aware fallthrough |
| `api.go` | `APIServer`: REST endpoints (Go 1.22+ routing), auth + metrics middleware |
| `grpc_server.go` | `GRPCServer`: List/Upsert/Delete RPCs, proto message conversion |
| `auth.go` | `Auth`: Bearer token + mTLS CN validation, HTTP middleware, gRPC unary interceptor |
| `record.go` | `Record` model: per-type validation (A/AAAA/CNAME/TXT/MX/SRV/NS/PTR/CAA), conversion to `dns.RR` |
| `tls_helper.go` | `buildTLSConfig`: server-only or mutual TLS (min TLS 1.2) |
| `metrics.go` | Prometheus counters for API/gRPC operations |
| `ready.go` | `Ready()` interface for CoreDNS readiness checks |
| `proto/dynupdate.proto` | gRPC service definition (`dynupdate.v1.DynUpdateService`) |

### Data Flow

- **DNS queries**: CoreDNS chain → `DynUpdate.ServeDNS()` → `Store.Get()` → answer or fallthrough
- **REST mutations**: HTTP → `APIServer` → auth middleware → `Store.Upsert/Delete` → atomic persist
- **gRPC mutations**: gRPC → auth interceptor → `GRPCServer` → `Store.Upsert/Delete` → atomic persist
- **Sync policies** (`SyncPolicy`): enforced inside Store mutation methods; denied operations return `ErrPolicyDenied`, mapped to HTTP 403 / gRPC `PermissionDenied`

### Auth Model

Auth is **fail-closed**: a `listen` directive without `token`, `allowed_cn`, or explicit `no_auth` causes a startup error. Bearer token and mTLS CN are dual-auth (either suffices). Token comparison is constant-time.

## Dependencies

| Dependency | Purpose |
|-----------|---------|
| `github.com/coredns/caddy` | Corefile parsing |
| `github.com/coredns/coredns` | Plugin infrastructure, DNS server config |
| `github.com/miekg/dns` | DNS wire protocol, RR types |
| `github.com/prometheus/client_golang` | Prometheus metrics |
| `google.golang.org/grpc` | gRPC framework |
| `google.golang.org/protobuf` | Protocol Buffers runtime |

## Proto Generation

Requires `protoc`, `protoc-gen-go`, and `protoc-gen-go-grpc` installed. Run `make proto` to regenerate from `proto/dynupdate.proto`.

## Examples Directory

- `Corefile.tailscale` / `Corefile.tls`: production Corefile configurations
- `dynupdate_watcher.py` / `dynupdate-watcher.sh`: network interface monitors that auto-upsert A/AAAA records

## Conventions

- All new files must start with a 2-line `// ABOUTME:` header comment
- Conventional Commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`
- No CI/CD pipelines; quality gate is `make check` (vet + lint + test)
- Work on `main` branch by default

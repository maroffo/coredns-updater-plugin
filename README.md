# dynupdate

## Name

*dynupdate* - dynamically manage DNS records via REST API and gRPC.

## Description

*dynupdate* is a CoreDNS plugin that allows authenticated clients to create, update, and delete DNS records at runtime through a REST API and gRPC interface. Records are stored in memory for fast lookups, backed by atomic JSON persistence for durability across restarts.

The plugin supports A, AAAA, CNAME, TXT, MX, SRV, NS, PTR, and CAA record types. CNAME chasing is built in: querying an alias automatically resolves the full chain within the plugin's store.

Authentication supports Bearer tokens and mTLS client certificate validation, applied to both REST and gRPC endpoints. **Authentication is fail-closed**: any `api` or `grpc` block with a `listen` directive must configure at least one auth method (`token`, `allowed_cn`) or explicitly opt out with `no_auth`.

## Syntax

```
dynupdate [ZONES...] {
    datafile    PATH
    reload      DURATION
    max_records N

    api {
        listen     ADDR
        token      SECRET
        tls        CERT KEY CA
        allowed_cn CN [CN...]
        no_auth
    }

    grpc {
        listen     ADDR
        token      SECRET
        tls        CERT KEY CA
        allowed_cn CN [CN...]
        no_auth
    }

    fallthrough [ZONES...]
}
```

- **ZONES** - the zones this plugin is authoritative for. Defaults to the server block zones.
- `datafile` **PATH** - (required) path to the JSON file for record persistence.
- `reload` **DURATION** - interval for checking external file modifications (e.g., `30s`). Disabled if omitted.
- `max_records` **N** - maximum number of records the store will hold. New inserts beyond this limit are rejected; updates to existing records are always allowed. A value of `0` (default) means unlimited.
- `api` - configure the REST API server. When `listen` is set, at least one of `token`, `allowed_cn`, or `no_auth` is **required**.
  - `listen` **ADDR** - address to bind (e.g., `:8080`).
  - `token` **SECRET** - Bearer token for authentication.
  - `tls` **CERT KEY CA** - TLS certificate, key, and optional CA for HTTPS. When CA is provided, mTLS with client certificate verification is enforced.
  - `allowed_cn` **CN...** - allowed client certificate Common Names (requires `tls` with CA).
  - `no_auth` - explicitly disable authentication. **Use with caution**; only appropriate for loopback or trusted-network deployments.
- `grpc` - configure the gRPC server. When `listen` is set, at least one of `token`, `allowed_cn`, or `no_auth` is **required**.
  - `listen` **ADDR** - address to bind (e.g., `:8443`).
  - `token` **SECRET** - Bearer token for authentication.
  - `tls` **CERT KEY CA** - TLS certificate, key, and optional CA for gRPC TLS. When CA is provided, mTLS with client certificate verification is enforced.
  - `allowed_cn` **CN...** - allowed client certificate Common Names (requires `tls` with CA).
  - `no_auth` - explicitly disable authentication.
- `fallthrough` **[ZONES...]** - if a query is not found, pass it to the next plugin. Optionally restricted to specific zones.

### Authentication Model

The `api` and `grpc` blocks use **fail-closed** authentication. If a `listen` address is configured, the plugin refuses to start unless one of the following is present:

| Directive | Effect |
|-----------|--------|
| `token SECRET` | Require `Authorization: Bearer SECRET` header |
| `allowed_cn CN...` | Require client certificate with matching Common Name (needs `tls` with CA) |
| `no_auth` | Explicitly allow unauthenticated access |

When both `token` and `allowed_cn` are configured, a request is authorized if **either** credential is valid. When `tls` includes a CA, all clients must present a valid certificate (mTLS); token-based auth operates as an additional layer on top.

### TLS Configuration

The `tls` directive accepts three positional arguments:

```
tls CERT KEY CA
```

| Argument | Purpose |
|----------|---------|
| **CERT** | Path to the server certificate (PEM) |
| **KEY** | Path to the server private key (PEM) |
| **CA** | Path to the CA certificate (PEM) for client verification. When provided, mTLS is enforced with `RequireAndVerifyClientCert`. Omit or pass an empty CA to use server-only TLS. |

The minimum TLS version is 1.2.

## Metrics

If monitoring is enabled (via the *prometheus* plugin), the following metrics are exported:

- `coredns_dynupdate_request_count_total{server}` - total DNS requests handled.
- `coredns_dynupdate_response_rcode_count_total{server, rcode}` - DNS responses by rcode.
- `coredns_dynupdate_api_request_count_total{method, status}` - REST API requests.
- `coredns_dynupdate_store_records{type}` - current number of records by type.

## Ready

This plugin reports readiness to the *ready* plugin. It is ready once the backing JSON file has been loaded (or created).

## Examples

### Minimal: REST API with Bearer Token

```corefile
example.org:53 {
    dynupdate example.org. {
        datafile /var/lib/coredns/records.json
        reload   30s

        api {
            listen :8080
            token  super-secret-token-here
        }

        fallthrough
    }

    cache 30
    forward . 8.8.8.8:53
    errors
    log
}
```

Create a record:

```bash
curl -H "Authorization: Bearer super-secret-token-here" \
     -X POST http://localhost:8080/api/v1/records \
     -d '{"name":"app.example.org.","type":"A","ttl":300,"value":"10.0.0.1"}'
```

Query via DNS:

```bash
dig @localhost app.example.org A
```

### mTLS: API and gRPC with Client Certificates

```corefile
example.org:53 {
    dynupdate example.org. {
        datafile    /var/lib/coredns/records.json
        reload      30s
        max_records 10000

        api {
            listen     :8443
            tls        /etc/coredns/server.pem /etc/coredns/server-key.pem /etc/coredns/ca.pem
            allowed_cn admin-client.example.org
        }

        grpc {
            listen     :8444
            tls        /etc/coredns/server.pem /etc/coredns/server-key.pem /etc/coredns/ca.pem
            allowed_cn grpc-client.example.org automation.example.org
        }

        fallthrough
    }

    cache 30
    forward . 8.8.8.8:53
    errors
    log
    prometheus
}
```

Create a record using mTLS:

```bash
curl --cert /etc/coredns/client.pem \
     --key /etc/coredns/client-key.pem \
     --cacert /etc/coredns/ca.pem \
     -X POST https://localhost:8443/api/v1/records \
     -d '{"name":"app.example.org.","type":"A","ttl":300,"value":"10.0.0.1"}'
```

### Development: Explicit No-Auth

For local development or trusted-network deployments where authentication is not needed:

```corefile
example.org:53 {
    dynupdate example.org. {
        datafile /tmp/records.json

        api {
            listen :8080
            no_auth
        }

        fallthrough
    }

    forward . 8.8.8.8:53
    errors
    log
}
```

> **Warning**: `no_auth` disables all authentication. Only use on loopback or in fully trusted networks.

## REST API

| Method | Path | Description |
|--------|------|-------------|
| GET    | `/api/v1/records` | List all records (optional `?name=` filter) |
| GET    | `/api/v1/records/{name}` | Get records for a name |
| POST   | `/api/v1/records` | Create/upsert a record |
| PUT    | `/api/v1/records` | Update a record (upsert) |
| DELETE | `/api/v1/records/{name}` | Delete all records for a name |
| DELETE | `/api/v1/records/{name}/{type}` | Delete records by name and type |

## gRPC API

Service: `dynupdate.v1.DynUpdateService`

| RPC | Request | Response |
|-----|---------|----------|
| `List` | `ListRequest{name}` | `ListResponse{records}` |
| `Upsert` | `UpsertRequest{record}` | `UpsertResponse{record}` |
| `Delete` | `DeleteRequest{name, type, value}` | `DeleteResponse{}` |

## Record Validation

Record names are validated beyond basic non-empty and trailing-dot checks:

- Consecutive dots (`..`) are rejected.
- Individual labels must not exceed 63 characters.
- The total name length must not exceed 253 characters.

Values passed via the gRPC API are bounds-checked before narrowing: `priority`, `weight`, and `port` must fit in uint16 (0-65535), and `flag` must fit in uint8 (0-255). Values exceeding these bounds return `InvalidArgument`.

## Building

Add the plugin to CoreDNS's `plugin.cfg`:

```
dynupdate:github.com/mauromedda/coredns-updater-plugin
```

Then build CoreDNS:

```bash
go generate
go build
```

## Migration from Pre-Auth Versions

The following change is **breaking** for existing configurations:

**Fail-closed authentication** — any `api` or `grpc` block that has a `listen` directive now **requires** at least one of `token`, `allowed_cn`, or `no_auth`. Previously, omitting all auth directives silently allowed unauthenticated access.

To migrate:

| Scenario | Action |
|----------|--------|
| Already using `token` or `allowed_cn` | No change needed |
| Intentionally open (dev/trusted) | Add `no_auth` to the block |
| Accidentally open | Add a `token` directive (recommended) |

Example fix for a previously open configuration:

```diff
 api {
     listen :8080
+    token  my-secret-token
 }
```

Or, if unauthenticated access is intentional:

```diff
 api {
     listen :8080
+    no_auth
 }
```

## See Also

The CoreDNS manual: https://coredns.io/manual/plugins-dev/

## Bugs

Report bugs at https://github.com/mauromedda/coredns-updater-plugin/issues.

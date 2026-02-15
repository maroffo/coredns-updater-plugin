# dynupdate

## Name

*dynupdate* - dynamically manage DNS records via REST API and gRPC.

## Description

*dynupdate* is a CoreDNS plugin that allows authenticated clients to create, update, and delete DNS records at runtime through a REST API and gRPC interface. Records are stored in memory for fast lookups, backed by atomic JSON persistence for durability across restarts.

The plugin supports A, AAAA, CNAME, TXT, MX, SRV, NS, PTR, and CAA record types. CNAME chasing is built in: querying an alias automatically resolves the full chain within the plugin's store.

Authentication supports Bearer tokens and mTLS client certificate validation, applied to both REST and gRPC endpoints.

## Syntax

```
dynupdate [ZONES...] {
    datafile PATH
    reload   DURATION

    api {
        listen ADDR
        token  SECRET
        tls    CERT KEY CA
        allowed_cn CN [CN...]
    }

    grpc {
        listen ADDR
        token  SECRET
        tls    CERT KEY CA
        allowed_cn CN [CN...]
    }

    fallthrough [ZONES...]
}
```

- **ZONES** - the zones this plugin is authoritative for. Defaults to the server block zones.
- `datafile` **PATH** - (required) path to the JSON file for record persistence.
- `reload` **DURATION** - interval for checking external file modifications (e.g., `30s`). Disabled if omitted.
- `api` - configure the REST API server.
  - `listen` **ADDR** - address to bind (e.g., `:8080`).
  - `token` **SECRET** - Bearer token for authentication.
  - `tls` **CERT KEY CA** - TLS certificate, key, and CA for HTTPS/mTLS.
  - `allowed_cn` **CN...** - allowed client certificate Common Names.
- `grpc` - configure the gRPC server.
  - `listen` **ADDR** - address to bind (e.g., `:8443`).
  - `token` **SECRET** - Bearer token for authentication.
  - `tls` **CERT KEY CA** - TLS certificate, key, and CA for gRPC TLS/mTLS.
  - `allowed_cn` **CN...** - allowed client certificate Common Names.
- `fallthrough` **[ZONES...]** - if a query is not found, pass it to the next plugin. Optionally restricted to specific zones.

## Metrics

If monitoring is enabled (via the *prometheus* plugin), the following metrics are exported:

- `coredns_dynupdate_request_count_total{server}` - total DNS requests handled.
- `coredns_dynupdate_response_rcode_count_total{server, rcode}` - DNS responses by rcode.
- `coredns_dynupdate_api_request_count_total{method, status}` - REST API requests.
- `coredns_dynupdate_store_records{type}` - current number of records by type.

## Ready

This plugin reports readiness to the *ready* plugin. It is ready once the backing JSON file has been loaded (or created).

## Examples

Serve `example.org` with a REST API on port 8080:

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

## See Also

The CoreDNS manual: https://coredns.io/manual/plugins-dev/

## Bugs

Report bugs at https://github.com/mauromedda/coredns-updater-plugin/issues.

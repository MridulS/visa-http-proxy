# visa-http-proxy (Go)

A minimal, **standard-library-only** reimplementation of the VISA Jupyter/HTTP proxy. It
authenticates incoming requests, resolves the target VISA instance from the URL
and the VISA API, optionally rewrites the path, and forwards HTTP and WebSocket
traffic to the instance.

It is a **clean** reimplementation: where the legacy Node proxy had quirks/bugs,
this does the RFC-correct / intended thing. The full behavioral contract — and
every intentional Node→Go divergence — is documented in
`../visa-jupyter-proxy/contract-tests/CONTRACT_BEHAVIORS.md`, and enforced by the
shared contract suite alongside it (105 tests, green against this binary — and
against the container image).

## No dependencies

`go.mod` has no `require` block — everything (HTTP/WebSocket proxying, unverified
JWT decode, JSON config, persistence) uses only the Go standard library.

## Build & run

```bash
go vet ./... && go test ./... && go build -o visa-proxy .
./visa-proxy        # reads ./proxy.conf.json from the cwd
```

## Configuration

`proxy.conf.json` (cwd-relative): a JSON array of handlers. `pathRewrite` order is
significant (rules apply in declaration order, first match wins).

```json
[
  { "match": "^/jupyter/(\\d+).*", "type": "jupyter", "name": "Jupyter", "remotePort": 8888, "ws": true },
  { "match": "^/visafs/(\\d+).*", "type": "service", "name": "Visa Files", "remotePort": 8090,
    "pathRewrite": { "^/visafs/\\d+/": "/" } }
]
```

Environment (same names/defaults as the Node implementation):

| Variable | Default | Meaning |
|---|---|---|
| `VISA_JUPYTER_PROXY_SERVER_HOST` | `0.0.0.0` | bind host |
| `VISA_JUPYTER_PROXY_SERVER_PORT` | `8088` | bind port |
| `VISA_JUPYTER_PROXY_API_HOST` | `localhost` | VISA API host |
| `VISA_JUPYTER_PROXY_API_PORT` | `8086` | VISA API port |
| `VISA_JUPYTER_PROXY_CACHE_REFRESH_TIME_S` | `60` | instance-cache TTL (seconds) |
| `VISA_JUPYTER_PROXY_STORAGE_DIR` | `data` | notebook-session persistence dir |

## Testing against the contract suite

The suite lives in the sibling `visa-jupyter-proxy` checkout and drives either
implementation through its public contract, so it needs only a path to a binary
(or, with `VISA_PROXY_IMAGE`, a container image).

```bash
go build -o visa-proxy .
cd ../visa-jupyter-proxy/contract-tests
VISA_PROXY_START_CMD="$(cd ../../visa-http-proxy && pwd)/visa-proxy" uv run pytest -W ignore::DeprecationWarning
```

## Layout

```
main.go        wiring, signals, graceful shutdown, startup zombie cleanup
config.go      env + proxy.conf.json (ordered pathRewrite decode)
server.go      shared ReverseProxy + HTTP/WS dispatch
handler.go     match -> auth/resolve -> forward
auth.go        Bearer header / access_token cookie extraction
jwt.go         unverified JWT decode -> employeeNumber
cache.go       (employeeNumber, instanceId) TTL cache (no singleflight)
instances.go   VISA API client (GET instance, notebook open/close)
notebook.go    Jupyter session lifecycle + zombie cleanup
store.go       mutex-guarded JSON session store
ws.go          hijack + dial + transparent io.Copy relay
```

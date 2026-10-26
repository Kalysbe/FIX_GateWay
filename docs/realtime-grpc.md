# Realtime: gRPC Bidirectional Streaming

Realtime uses `fixgateway.realtime.v1.RealtimeService/Stream`, a native HTTP/2 bidirectional gRPC stream with Protocol Buffers. Contract: `backend/proto/realtime/v1/realtime.proto`; generated Go client: `backend/gen/realtime/v1`. Broker order mutations remain REST and the exchange adapters remain FIX. Brokers can also subscribe to read-only events through native gRPC.

The gRPC runtime is pinned to stable v1.83.2, which includes the [GO-2026-6443 fix](https://pkg.go.dev/vuln/GO-2026-6443). TLS is mandatory, minimum TLS 1.3. Clients verify the server hostname and certificate using system trust or the operator's CA. Mutual TLS and HMAC are disabled by default. Admin streams require a current admin JWT in `authorization: Bearer <JWT>` metadata; broker streams require an API token over the VPN, with optional `x-client-type: broker`. See [broker subscriptions](grpc-subscriptions.md). Enabling `GRPC_MTLS_REQUIRED=true` additionally requires a clientAuth certificate issued by the configured CA; it grants no application access without a token. Role, status and session revision are checked against the database initially and every five seconds (two-second database timeout). Expiry and account changes terminate the stream. Never disable server verification or send tokens in URLs.

## Protocol

1. Open `Stream` with verified TLS and Bearer metadata: API token for brokers, JWT for admins.
2. Send `ClientMessage.subscribe` within five seconds. `event_types` empty means all events allowed for this role. Explicit forbidden or unknown topics return `PermissionDenied`.
3. Receive `ServerMessage.ready`, then `event` messages. There is no initial snapshot: load current state over REST, then apply events. Events include sequence, timestamp, type and a Protobuf Struct containing the existing event DTO. Decimal values represented as strings remain strings; Struct numeric values use double precision.
4. Send another subscribe to replace the subscription or ping to receive a matching pong. Space commands by at least 100 ms. Client half-close preserves server event delivery until cancellation or session termination.

Events: `obs.event`, `notification.new`, `order.updated`, `trade.created`, `positions.updated`, `marketdata.tick`, `adapter.status_changed`. `obs.event` requires an audit-authorized role; READ_ONLY cannot receive it. Unknown event types default to denied.

`marketdata.tick` carries one changed row of the QUIK `Params` table verbatim in `data_json` (`data` is empty; `removed=true` marks a row that disappeared), the same for brokers and admins. See [market data](market-data.md).

Limits: incoming message 16 KiB, outgoing message 8 MiB, 128 concurrent streams per connection, 1,024 active subscriptions, queue 64 events per subscription. Slow consumers disconnect instead of blocking producers; sends have a five-second deadline. Server sends heartbeat pong every 20 seconds. There is no replay or resume guarantee. Refresh REST state after reconnect; sequence is global and role filtering can create gaps.

## Browser console

Browsers cannot directly use native gRPC, and [gRPC-Web supports server streaming but not bidirectional streaming](https://github.com/grpc/grpc-web). The React console therefore uses authenticated fetch SSE at `GET /api/v1/admin/events/stream`. This adapter opens the same native TLS gRPC stream (mTLS when enabled), forwards JWT metadata and translates Protobuf events to SSE. There is no parallel WebSocket backend. The console reconnects with backoff and refreshes cached REST state. Nginx buffering is disabled for this route. Protect the browser connection with HTTPS in production; native gRPC TLS does not provide HTTPS for the web frontend.

## Local development

`docker compose -f deploy/docker-compose.yml up --build` creates development certificates in the `grpc-certs` volume using the one-shot `devcerts` service. Native gRPC is exposed at `localhost:8443`. Existing containers need rebuilding and restarting to use this change.

Outside Compose, run `go run ./cmd/devcerts -out certs -dns localhost,gateway` from `backend` before starting the gateway. Native development clients use `certs/ca.crt` to verify the server; `bridge.crt`/`bridge.key` are only needed when mTLS is enabled. Keep keys private. Generated files are excluded from Git. Certificates expire after 30 days; the generator verifies existing files and refuses to overwrite partial or invalid PKI. Rotate all development files/volume intentionally when necessary. The CA private key is never persisted.

Configuration:

```dotenv
GRPC_ADDR=:8443
GRPC_TLS_CERT=certs/server.crt
GRPC_TLS_KEY=certs/server.key
GRPC_TLS_CA=certs/ca.crt
GRPC_MTLS_REQUIRED=false
GRPC_CLIENT_CERT=
GRPC_CLIENT_KEY=
HMAC_REQUIRED=false
GRPC_TARGET=localhost:8443
GRPC_SERVER_NAME=localhost
```

`GRPC_CLIENT_*` belongs to the HTTP-to-gRPC adapter. Leave both empty for TLS-only operation. When `GRPC_MTLS_REQUIRED=true`, they default to `bridge.crt`/`bridge.key` beside the server certificate; explicit paths can override them. Token authorization remains mandatory. Unencrypted gRPC is never offered.

## grpcui: interactive documentation and testing (Windows)

grpcui v1.5.4 is installed at `%LOCALAPPDATA%\grpcui\grpcui.exe` from the [official release](https://github.com/fullstorydev/grpcui/releases/tag/v1.5.4), with its archive SHA-256 checked against the release checksums.

With the local gateway running, start from the repository root:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/start-grpcui.ps1
```

Open http://localhost:8090. The UI loads `backend/proto/realtime/v1/realtime.proto` directly (server reflection is not required) and connects to `localhost:8443` with verified TLS. Request forms describe the service, messages, and fields. Override `-Target`, `-Port`, or `-ServerName` on the script when needed. Add `-MutualTLS` only when client certificates are required by the gateway.

The script expects `ca.crt` in the Git-ignored `backend/certs` folder. After certificate rotation, refresh it from the appropriate gateway container:

```powershell
docker cp fix_gateway-gateway-1:/certs/ca.crt backend/certs/ca.crt
```

With `-MutualTLS`, also provide the client certificate and key:

```powershell
docker cp fix_gateway-gateway-1:/certs/bridge.crt backend/certs/bridge.crt
docker cp fix_gateway-gateway-1:/certs/bridge.key backend/certs/bridge.key
```

Select `fixgateway.realtime.v1.RealtimeService` / `Stream`. In request metadata replace `Bearer <JWT>` with your broker API token or current admin JWT. In Raw Request, send an array: `[{"subscribe":{"event_types":["marketdata.tick"]}}]`. A streaming method requires an array even for a single message. grpcui collects streaming responses until completion or the configured 30-second deadline, so this UI is suitable for bounded stream tests. The deadline is expected for a continuously running subscription. A missing/expired token returns `Unauthenticated`. Stop the foreground process with Ctrl+C. Only localhost can access this UI.

## Production

Provision the server TLS certificate, key and trusted CA. Mount them read-only and set matching SAN/server name. Restrict REST and gRPC access to broker VPN addresses using host bindings and firewall rules. Replace the development certificate service and volume with operator-provisioned secrets; the base Compose file is for development. For optional mTLS, additionally issue separate clientAuth certificates to the internal bridge and each broker. Do not use the development CA or shared bridge key for external production clients. Rotate certificates and restart the gateway to load replacements; certificate hot reload is not implemented. Follow existing production requirements for HTTP HTTPS, trusted proxies, strong secrets and database TLS.

Missing or invalid certificate files abort startup with a nonzero exit code. Removed endpoints `/api/v1/admin/ws` and `/api/v1/admin/ws-ticket` are no longer available; clients must migrate.

## Regenerate client/server bindings

Orders use `Event.data_json` for the original `Orders` row JSON, including exact
PostgreSQL numeric values and nulls. `Event.data` is unset for `order.updated`.
`Event.removed=true` removes that row from the current set without modifying its
JSON or assigning a terminal trading status. Other event types retain `data`.

Run `sh scripts/generate-proto.sh` inside a Go 1.26.7 Debian container with `backend` mounted as working directory. The script installs protoc and pinned Go plugins (`protoc-gen-go v1.36.11`, `protoc-gen-go-grpc v1.6.2`). Generated bindings are checked into the project so normal builds do not need protoc. Native clients in other languages can generate bindings from the same `.proto`.

# Realtime: gRPC Bidirectional Streaming

Admin realtime uses `fixgateway.realtime.v1.RealtimeService/Stream`, a native HTTP/2 bidirectional gRPC stream with Protocol Buffers. Contract: `backend/proto/realtime/v1/realtime.proto`; generated Go client: `backend/gen/realtime/v1`. Existing broker order APIs remain REST and the exchange adapters remain FIX.

The gRPC runtime is pinned to stable v1.83.2, which includes the [GO-2026-6443 fix](https://pkg.go.dev/vuln/GO-2026-6443). TLS is mandatory, minimum TLS 1.3. Mutual TLS is enabled by default. Clients must trust the configured CA, verify the server hostname and supply a certificate with clientAuth usage issued by that CA. A certificate alone grants no application access: every stream also requires a current admin JWT in `authorization: Bearer <JWT>` metadata. Role, status and session revision are checked against the database initially and every five seconds (two-second database timeout). Expiry and account changes terminate the stream. Never disable server verification or send tokens in URLs.

## Protocol

1. Open `Stream` with TLS credentials and JWT metadata.
2. Send `ClientMessage.subscribe` within five seconds. `event_types` empty means all events allowed for this role. Explicit forbidden or unknown topics return `PermissionDenied`.
3. Receive `ServerMessage.ready`, then `event` messages. Subscriptions including `marketdata.tick` immediately receive the current in-memory FIX quote snapshot when available, followed by updates. The backend owns the continuous FIX subscription independently of UI clients and restores it after reconnect; UI refreshes read this subscription without replacing it. Events include sequence, timestamp, type and a Protobuf Struct containing the existing event DTO. Decimal values represented as strings remain strings; Struct numeric values use double precision.
4. Send another subscribe to replace the subscription or ping to receive a matching pong. Space commands by at least 100 ms. Client half-close preserves server event delivery until cancellation or session termination.

Events: `obs.event`, `notification.new`, `order.updated`, `trade.created`, `positions.updated`, `marketdata.tick`, `adapter.status_changed`. `obs.event` requires an audit-authorized role; READ_ONLY cannot receive it. Unknown event types default to denied.

Limits: incoming message 16 KiB, outgoing message 8 MiB, 128 concurrent streams per connection, 1,024 active subscriptions, queue 64 events per subscription. Slow consumers disconnect instead of blocking producers; sends have a five-second deadline. Server sends heartbeat pong every 20 seconds. There is no replay or resume guarantee. Refresh REST state after reconnect; sequence is global and role filtering can create gaps.

## Browser console

Browsers cannot directly use native gRPC, and [gRPC-Web supports server streaming but not bidirectional streaming](https://github.com/grpc/grpc-web). The React console therefore uses authenticated fetch SSE at `GET /api/v1/admin/events/stream`. This adapter opens the same native mTLS gRPC stream, forwards JWT metadata and translates Protobuf events to SSE. There is no parallel WebSocket backend. The console reconnects with backoff and refreshes cached REST state. Nginx buffering is disabled for this route. Protect the browser connection with HTTPS in production; native gRPC TLS does not provide HTTPS for the web frontend.

## Local development

`docker compose -f deploy/docker-compose.yml up --build` creates development certificates in the `grpc-certs` volume using the one-shot `devcerts` service. Native gRPC is exposed at `localhost:8443`. Existing containers need rebuilding and restarting to use this change.

Outside Compose, run `go run ./cmd/devcerts -out certs -dns localhost,gateway` from `backend` before starting the gateway. For native development clients use `certs/ca.crt` and the generated `bridge.crt`/`bridge.key`. Keep keys private. Generated files are excluded from Git. Certificates expire after 30 days; the generator verifies existing files and refuses to overwrite partial or invalid PKI. Rotate all development files/volume intentionally when necessary. The CA private key is never persisted.

Configuration:

```dotenv
GRPC_ADDR=:8443
GRPC_TLS_CERT=certs/server.crt
GRPC_TLS_KEY=certs/server.key
GRPC_TLS_CA=certs/ca.crt
GRPC_MTLS_REQUIRED=true
GRPC_CLIENT_CERT=certs/bridge.crt
GRPC_CLIENT_KEY=certs/bridge.key
GRPC_TARGET=localhost:8443
GRPC_SERVER_NAME=localhost
```

`GRPC_CLIENT_*` belongs to the HTTP-to-gRPC adapter. For TLS-only operation set `GRPC_MTLS_REQUIRED=false`; JWT authorization remains mandatory. Unencrypted gRPC is never offered.

## grpcui: interactive documentation and testing (Windows)

grpcui v1.5.4 is installed at `%LOCALAPPDATA%\grpcui\grpcui.exe` from the [official release](https://github.com/fullstorydev/grpcui/releases/tag/v1.5.4), with its archive SHA-256 checked against the release checksums.

With the local gateway running, start from the repository root:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/start-grpcui.ps1
```

Open http://localhost:8090. The UI loads `backend/proto/realtime/v1/realtime.proto` directly (server reflection is not required) and connects to `localhost:8443` with verified TLS and the development client certificate. Request forms describe the service, messages, and fields. Override `-Target`, `-Port`, or `-ServerName` on the script when needed.

The script expects `ca.crt`, `bridge.crt`, and `bridge.key` in the Git-ignored `backend/certs` folder. For the current Compose stack these were copied from the running gateway. After certificate rotation, refresh them from the appropriate container:

```powershell
docker cp fix_gateway-gateway-1:/certs/ca.crt backend/certs/ca.crt
docker cp fix_gateway-gateway-1:/certs/bridge.crt backend/certs/bridge.crt
docker cp fix_gateway-gateway-1:/certs/bridge.key backend/certs/bridge.key
```

Select `fixgateway.realtime.v1.RealtimeService` / `Stream`. In request metadata replace `Bearer <JWT>` with your current admin JWT from an authenticated admin session. In Raw Request, send an array: `[{"subscribe":{"event_types":["marketdata.tick"]}}]`. A streaming method requires an array even for a single message. grpcui collects streaming responses until completion or the configured 30-second deadline, so this UI is suitable for bounded stream tests. The deadline is expected for a continuously running subscription. A missing/expired JWT returns `Unauthenticated`. Stop the foreground process with Ctrl+C. Only localhost can access this UI.

## Production

Provision server and bridge client certificates from your organisation's CA. Mount them read-only, set matching SAN/server name and restrict port exposure. Replace the development certificate service and volume with operator-provisioned secrets in your deployment configuration; the supplied Compose file is for development. Do not use its development CA or shared bridge client key for external production clients. Issue separate certificates to those clients. Rotate certificates and restart the gateway to load replacements; certificate hot reload is not implemented. Follow existing production requirements for HTTP HTTPS, trusted proxies, strong secrets and database TLS.

Missing or invalid certificate files abort startup with a nonzero exit code. Removed endpoints `/api/v1/admin/ws` and `/api/v1/admin/ws-ticket` are no longer available; clients must migrate.

## Regenerate client/server bindings

Run `sh scripts/generate-proto.sh` inside a Go 1.26.7 Debian container with `backend` mounted as working directory. The script installs protoc and pinned Go plugins (`protoc-gen-go v1.36.11`, `protoc-gen-go-grpc v1.6.2`). Generated bindings are checked into the project so normal builds do not need protoc. Native clients in other languages can generate bindings from the same `.proto`.

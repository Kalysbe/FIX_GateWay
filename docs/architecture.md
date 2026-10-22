# Architecture — Broker FIX Gateway

## Overview

The Gateway sits between broker HTTPS clients and one or more FIX Adapter instances connected to QUIK.

```text
Broker → REST API → Order Service → PostgreSQL (+ outbox) → Worker
       → Load Balancer → Adapter Manager → Adapter interface → QuickFIX/Go → ARQA/Simulator → QUIK
```

## Design principles

1. **Adapter replaceability** — business logic depends only on `adapters.Adapter`.
2. **No hard-coded adapter count** — pool is loaded from DB/config.
3. **Consistency first** — order create and outbox insert share one DB transaction.
4. **No duplicate orders** — unique `(broker_id, client_order_id)`, stable `fix_cl_ord_id`, send guards.
5. **Stateless process** — durable state in PostgreSQL; multiple Gateway instances can share the outbox via `FOR UPDATE SKIP LOCKED`.

## Components

| Component | Responsibility |
|-----------|----------------|
| API Layer | REST, auth middleware, OpenAPI, native gRPC bidirectional streaming + browser SSE adapter |
| Broker Auth | Bearer token → hash → broker context |
| Order Service | validation, idempotency, lifecycle events |
| Outbox Worker | claim pending events, route, send, retry |
| Load Balancer | least connections / WRR / RR + health filter |
| Adapter Manager | start/stop/health/failover pool |
| ARQA Adapter | QuickFIX/Go initiator + message mapping |
| Admin UI | React console for ops |

## Outbound queue abstraction

### Required durability contract (2026-10-02)

- Accepted trading requests must never be lost. Persist the request and its queue entry atomically before acknowledging acceptance to the broker; an in-memory queue alone is insufficient.
- Keep every accepted request durably tracked until a confirmed terminal outcome. Overload, process restarts, database or FIX outages, and retry exhaustion must not silently discard requests. Requests requiring reconciliation or operator action remain durably tracked with an explicit state.
- An uncertain FIX send outcome requires reconciliation, not blind retransmission. Queue durability must preserve the existing duplicate-send protections.
- If durable enqueueing is unavailable, do not acknowledge acceptance. Return an explicit retryable failure; the broker must retain the request and retry with the same idempotency key. A rejected or disconnected HTTP request cannot be guaranteed queued by the gateway.
- Verify these requirements with crash, outage, overload, and recovery tests before claiming the guarantee is implemented.

`outbox.Dispatcher` isolates producers from transport. MVP uses PostgreSQL outbox. A future RabbitMQ/Redis implementation can replace the worker transport without changing Order Service create semantics (same transactional write to an outbound table/bridge).

## Failover rules

- Pre-send failure (adapter offline **before** the send fence) → clear route, requeue, select another healthy adapter.
- After `fix_send_attempted_at` is set, a successful or failed `SendOrder`, `SENT`, or external accept (`quik_order_id` / ACK+) → **no** automatic resend and **no** failover. At-most-once on the wire.
- Stale outbox rows left in `PROCESSING` (worker crash) are reclaimed after 2 minutes. Sends without a durable send fence return to `PENDING`; fenced sends and cancels remain stored as `FAILED` with `RECONCILIATION_REQUIRED` until their external outcome can be reconciled. They are never blindly retransmitted.
- Transient pre-send failures retry indefinitely with backoff capped at 30 seconds. Failed outcome updates are logged and leave the durable row recoverable.
- Worker startup and committed order/cancel enqueueing trigger a coalesced wake signal; full batches continue draining immediately. Periodic DB polling still recovers work independently of in-memory hints.
- Last online adapter cannot be disabled (HTTP 409).

## Overload protection

REST API admission reserves separate capacities for reads (32 concurrent requests) and trading submissions (16), before DB authentication. `API_READ_CONCURRENCY`, `API_TRADE_CONCURRENCY`, and `API_REQUEST_TIMEOUT` (15s) configure these limits. Overload returns 503 with `Retry-After`; context deadlines bound database acquisition and query waits. Health, readiness, metrics, and the long-lived SSE stream are excluded from normal request admission; readiness uses a short database timeout. The shared PostgreSQL pool remains a shared resource, so admission reservation does not guarantee trading latency under database saturation.

Per-token read and trading rate budgets are independent: by default 100 reads/second and 20 trading submissions/second (create and cancel combined). Existing creation limits also apply. Broker rate changes update existing buckets without replenishing exhausted tokens.

Observability notifications are coalesced in a bounded queue and persisted by a separate worker. Diagnostic overflow has counters and throttled warnings; diagnostics may be dropped during overload. Trading durability is provided by the canonical order and outbox transaction, independently of these diagnostic logs.

The adapter registry lock protects map membership only: dictionary fetches and adapter lifecycle network operations run outside it. Slow FIX requests must not block routing and readiness through an outstanding registry writer. Docker proxies resolve gateway addresses periodically to recover after container replacement.

## Related docs

- [API для брокеров: REST + gRPC](./broker-api-guide.md)
- [ARQA requirements checklist](./arqa-requirements-checklist.md)
- [ARQA FIX contract](./arqa-fix-contract.md)
- [Deployment](./deployment.md)

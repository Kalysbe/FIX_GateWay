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
| API Layer | REST, auth middleware, OpenAPI, WebSocket |
| Broker Auth | Bearer token → hash → broker context |
| Order Service | validation, idempotency, lifecycle events |
| Outbox Worker | claim pending events, route, send, retry |
| Load Balancer | least connections / WRR / RR + health filter |
| Adapter Manager | start/stop/health/failover pool |
| ARQA Adapter | QuickFIX/Go initiator + message mapping |
| Admin UI | React console for ops |

## Outbound queue abstraction

`outbox.Dispatcher` isolates producers from transport. MVP uses PostgreSQL outbox. A future RabbitMQ/Redis implementation can replace the worker transport without changing Order Service create semantics (same transactional write to an outbound table/bridge).

## Failover rules

- Pre-send failure → clear route, requeue, select another healthy adapter.
- After `SENT` / external accept (`quik_order_id` or ACK+) → **no** automatic resend to another adapter.
- Last online adapter cannot be disabled (HTTP 409).

## Related docs

- [ARQA requirements checklist](./arqa-requirements-checklist.md)
- [ARQA FIX contract](./arqa-fix-contract.md)
- [Deployment](./deployment.md)

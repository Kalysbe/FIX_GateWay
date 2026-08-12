# ARQA FIX Adapter — Working Contract (Gateway ↔ Adapter)

This document is the **working contract** used by `internal/adapters/arqa`.
Fields marked **ASSUMPTION** must be replaced when official ARQA documentation arrives.
See [`arqa-requirements-checklist.md`](./arqa-requirements-checklist.md).

## 1. Roles

| Role | Component |
|------|-----------|
| FIX Initiator | Broker FIX Gateway (`QuickFIX/Go`) |
| FIX Acceptor | ARQA FIX Adapter (or local `fixsimulator`) |
| Downstream | QUIK (via ARQA) |

Gateway never talks to QUIK directly. Business logic never imports QuickFIX types.

```text
OrderService → AdapterManager → Adapter interface → arqa.Adapter → QuickFIX session → ARQA → QUIK
```

## 2. Session (ASSUMPTION until confirmed)

| Parameter | Value |
|-----------|-------|
| BeginString | `FIX.4.2` (confirmed against QuikFIXAdapterService1); local `fixsimulator` still uses `FIX.4.4` |
| ConnectionType | initiator |
| HeartBtInt | `30` |
| ReconnectInterval | `5` |
| Default StartTime/EndTime | `00:00:00`-`00:00:00` (24×7) |
| ResetOnLogon | configurable (`ADAPTER_RESET_ON_LOGON`, default `N` for prod, `Y` for simulator) |
| Auth | Logon `Username` (553) / `Password` (554) when configured |
| TLS | optional via config |

Each adapter **instance** has its own session: unique `SenderCompID`, host, port, credentials.

Minimum concurrent instances in MVP: **2**.

## 3. Health model

| Layer | Meaning | AdapterStatus impact |
|-------|---------|----------------------|
| Process | Go adapter goroutines running | STARTING / STOPPING / OFFLINE |
| TCP | Socket connected | OFFLINE if down |
| FIX session | Logged on | ONLINE when logged on |
| QUIK link | ARQA↔QUIK (if signal available) | DEGRADED if session up, QUIK down |

**ASSUMPTION:** Until ARQA documents QUIK-link signalling, health is session-based:

- Logged on → `ONLINE`
- Not logged on → `OFFLINE`
- Starting/stopping → `STARTING` / `STOPPING`

## 4. Supported application messages (MVP)

### Outbound (Gateway → ARQA)

#### NewOrderSingle (MsgType=D)

| Tag | Field | Required | Source |
|-----|-------|----------|--------|
| 11 | ClOrdID | Y | Gateway-generated FIX ClOrdID (correlated to internal order) |
| 21 | HandlInst | Y | `1` (automated, no broker intervention) |
| 54 | Side | Y | 1=Buy, 2=Sell |
| 60 | TransactTime | Y | UTC, seconds precision (`YYYYMMDD-HH:MM:SS`) |
| 40 | OrdType | Y | 1=Market, 2=Limit |
| 44 | Price | C | Required for Limit (as supplied, no forced scale) |
| 38 | OrderQty | Y | As supplied |
| 1 | Account | Y | Торговый код из `account_id` заявки |
| 22 | SecurityIDSource | Y | Always `8`=Exchange Symbol (confirmed against ARQA sample) |
| 48 | SecurityID | Y | Instrument code / ISIN from `symbol` |
| 100 | ExDestination | Y | Код класса (`board` из заявки), e.g. `LISTEDSTOCKK` |
| 15 | Currency | N | If mapped |
| 59 | TimeInForce | N | Default Day (0) |
| 109 | ClientID | Y | Произвольный `client_code` из body заявки |
| 5018 | Firm (custom) | Y | `quik_firm` с API-токена (не из body) |

Not sent: `55` Symbol, `207` SecurityExchange (class code is tag **100** only).

#### OrderCancelRequest (MsgType=F)

| Tag | Field | Required |
|-----|-------|----------|
| 11 | ClOrdID | Y (new cancel id) |
| 41 | OrigClOrdID | Y |
| 37 | OrderID | C if known |
| 55 | Symbol | Y |
| 54 | Side | Y |
| 60 | TransactTime | Y |

#### OrderCancelReplaceRequest (MsgType=G)

| Tag | Field | Required |
|-----|-------|----------|
| 11 | ClOrdID | Y |
| 41 | OrigClOrdID | Y |
| 38 | OrderQty | Y |
| 44 | Price | C |
| 40 | OrdType | Y |
| 55 | Symbol | Y |
| 54 | Side | Y |
| 60 | TransactTime | Y |

### Inbound (ARQA → Gateway)

#### ExecutionReport (MsgType=8)

Correlated primarily by `ClOrdID` (11), secondarily by `OrderID` (37).

| ExecType (150) / OrdStatus (39) | Gateway order status |
|---------------------------------|----------------------|
| 0 New / 0 New | ACKNOWLEDGED |
| 1 Partial / 1 Partial | PARTIALLY_FILLED |
| 2 Fill / 2 Filled | FILLED |
| 4 Canceled / 4 Canceled | CANCELLED |
| 5 Replace / 5 Replaced | ACKNOWLEDGED (updated qty/price) |
| 8 Rejected / 8 Rejected | REJECTED |
| C Expired / C Expired | EXPIRED |
| D Restated | keep current; update fields; emit event |

Trades: when `LastQty` (32) > 0, create `executions` row (ExecID 17 unique).

#### OrderCancelReject (MsgType=9)

→ order event `CANCEL_REJECTED`; status returns from `CANCEL_PENDING` to previous open status.

## 5. Identity & anti-duplicate rules

| ID | Scope | Notes |
|----|-------|-------|
| `internal_order_id` | Gateway global | UUID |
| `client_order_id` | Unique per `broker_id` | Broker-provided idempotency key |
| `fix_cl_ord_id` | Per FIX session | Sent as tag 11; stable for retries of same send attempt |
| `quik_order_id` | From ER tag 37 | Set on ACK; blocks adapter reassignment resend |

**Hard rules:**

1. Outbox is at-least-once; worker must not send a new ClOrdID after `SENT` if external acceptance is unknown.
2. Failover to another adapter is allowed only before external accept (`quik_order_id` empty and status not ACK+).
3. Duplicate broker `client_order_id` returns the original order (HTTP 200), does not enqueue a second send.

## 6. Configuration surface (per adapter instance)

Stored in DB (non-secret) + ENV/secrets (secret):

```text
id, name, type=arqa|simulator
host, port, enabled, priority, weight, max_concurrent_orders
begin_string, sender_comp_id, target_comp_id
heartbeat_interval, reset_on_logon
username_env, password_env   # names of env vars holding secrets
```

Secrets never exposed on Admin API/UI.

## 7. Local simulator contract

`cmd/fixsimulator` accepts FIX.4.4 initiator connections and:

1. Accepts Logon
2. On NewOrderSingle → ExecutionReport NEW, then optional FILL (configurable)
3. On Cancel → ExecutionReport CANCELED
4. On invalid symbol `REJECT` → ExecutionReport Rejected

Used for Docker Compose e2e with two listener ports (two adapter instances).

## 8. Change control

When official ARQA dictionary arrives:

1. Update this contract (remove ASSUMPTION markers).
2. Replace/extend QuickFIX data dictionary.
3. Adjust tag mapping in `internal/adapters/arqa` only.
4. Do **not** change Order Service or HTTP DTOs unless domain fields are missing.

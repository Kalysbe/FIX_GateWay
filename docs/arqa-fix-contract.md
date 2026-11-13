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
| StartTime/EndTime | not set: QuickFIX never ends the session itself, so it never resets the numbers (before 05.11.2026, `00:00:00`-`00:00:00` UTC reset them every day at 06:00 Bishkek). `start_time`/`end_time` in the transport config set a schedule, both or neither |
| Daily reset | the QUIK adapter starts from 1 when it starts at 09:05 (seen 06.11.2026). The first logon of the working day carries 141=Y; a logon we refuse with "MsgSeqNum too low", or three logons in a row without success, make the next one carry 141=Y as well |
| Working hours | the QUIK adapters are off 04:00-09:05 Bishkek. The adapter manager runs the sessions only inside `FIX_SESSION_HOURS` (default `09:05-03:59`): Logout at 03:59, no connection attempts until 09:05 |
| ResetOnLogon | `adapters.reset_on_logon` per adapter. With `N` the numbers continue across logons and the gap is recovered with ResendRequest (35=2) / SequenceReset-GapFill (35=4); both QUIK adapters run so since 05.11.2026. With `Y` (141=Y) the QUIK adapter also clears its store of sent messages |
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
| 40 | OrdType | Y | 2=Limit if the request has `Price`, else 1=Market |
| 44 | Price | C | Required for Limit (as supplied, no forced scale) |
| 38 | OrderQty | Y | As supplied |
| 1 | Account | Y | Торговый счёт из `Account` заявки |
| 22 | SecurityIDSource | Y | Always `8`=Exchange Symbol (confirmed against ARQA sample) |
| 48 | SecurityID | Y | Instrument code / ISIN from `SecCode` |
| 100 | ExDestination | Y | Код класса (`ClassCode` из заявки), e.g. `LIST_GBA` |
| 15 | Currency | N | If mapped |
| 59 | TimeInForce | N | Always Day (0) |
| 109 | ClientID | Y | `ClientCode` из body заявки |
| 5018 | Firm (custom) | Y | `quik_firm` с API-токена (не из body) |

Not sent: `55` Symbol, `207` SecurityExchange (class code is tag **100** only).

#### OrderCancelRequest (MsgType=F)

| Tag | Field | Required |
|-----|-------|----------|
| 11 | ClOrdID | Y (new cancel id) |
| 41 | OrigClOrdID | Y |
| 37 | OrderID | C if known |
| 1 | Account | C if set on the order |
| 109 | ClientID | C if set on the order |
| 54 | Side | Y |
| 60 | TransactTime | Y |
| 22 | SecurityIDSource | Y (always `8`, as in NewOrderSingle) |
| 48 | SecurityID | Y |
| 100 | ExDestination | C if the order has a class code |
| 38 | OrderQty | Y (remaining quantity; full quantity if nothing remains) |
| 5018 | Firm (custom) | C if set on the order |

Not sent: `55` Symbol, `207` SecurityExchange (same instrument identification as NewOrderSingle).

#### OrderCancelReplaceRequest (MsgType=G)

**Not implemented:** `ReplaceOrder` returns an error and this message is never sent. The table is a planned shape only.

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
| 5 Replace / 5 Replaced | Not applied: report retained for venue reconciliation |
| 8 Rejected / 8 Rejected | REJECTED |
| C Expired / C Expired | EXPIRED |
| D Restated | keep current; update fields; emit event |

ExecType `G` (trade correct), `H` (trade cancel) and ExecTransType (20) other than `0` are not applied either: the report is retained for venue reconciliation.

Trades: when `LastQty` (32) > 0, create `executions` row (ExecID 17 unique).

#### OrderCancelReject (MsgType=9)

→ order event `CANCEL_REJECTED`; status returns from `CANCEL_PENDING` to previous open status.

## 5. Identity & anti-duplicate rules

| ID | Scope | Notes |
|----|-------|-------|
| `internal_order_id` | Gateway global | UUID |
| `client_order_id` | Unique per `broker_id` | Generated by the gateway (equals order `id`) |
| `idempotency_key` | Unique per `broker_id` when set | Not set since 30.10.2026: the REST API no longer reads `Idempotency-Key` |
| `fix_cl_ord_id` | Per FIX session | Sent as tag 11; stable for retries of same send attempt |
| `quik_order_id` | From ER tag 37 | Set on ACK; blocks adapter reassignment resend |

**Hard rules:**

1. Outbox is at-least-once; worker must not send a new ClOrdID after `SENT` if external acceptance is unknown.
2. Failover to another adapter is allowed only before external accept (`quik_order_id` empty and status not ACK+).
3. Every broker `POST /api/v1/orders` is a new order (user decision 30.10.2026: no `Idempotency-Key`). After an uncertain HTTP result the broker checks `GET /api/v1/orders` before sending again.

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

# ARQA FIX Adapter — Requirements Checklist

Materials to obtain from ARQA Technologies / the hosting broker before production FIX sessions.
Request documentation via ARQA sales (`sales@arqatech.com`) and broker ops.

## Status legend

- `[ ]` not received
- `[~]` partially known / assumed
- `[x]` confirmed

## 1. Protocol & dictionary

| Item | Status | Notes |
|------|--------|-------|
| FIX BeginString / version | [x] | Confirmed `FIX.4.2` via Logon handshake on `192.168.0.89:15101` (FIX.4.4 gets no reply) |
| Application version (if FIXT.1.1) | [ ] | |
| Official Data Dictionary XML | [ ] | Including custom tags |
| List of custom/user-defined tags | [ ] | Board, firm, QUIK-specific fields |
| Supported character encoding | [ ] | Usually ASCII / UTF-8 subset |

## 2. Session parameters

| Item | Status | Notes |
|------|--------|-------|
| SenderCompID (per adapter instance) | [x] | Gateway initiator: `TEST` (port 15101), `BROKER2` (port 15103) — equals adapter TargetCompID |
| TargetCompID | [x] | Gateway initiator: `QUIK` — equals adapter SenderCompID |
| SenderSubID / TargetSubID / LocationID | [ ] | If required |
| Logon authentication method | [ ] | Username/Password tags, custom password tag, SSL client cert |
| HeartBtInt | [~] | Default assumption: 30s |
| ResetOnLogon / ResetOnLogout / ResetOnDisconnect | [~] | Adapter Logon reply includes `141=Y`; Gateway UAT uses `reset_on_logon=true` |
| StartTime / EndTime / TimeZone | [ ] | |
| UseDataDictionary / ValidateFieldsOutOfOrder | [ ] | |
| Sequence number persistence expectations | [ ] | |

## 3. Connectivity

| Item | Status | Notes |
|------|--------|-------|
| Test host + port | [x] | `192.168.0.89:15101` (Service1 primary, TargetCompID=TEST), `:15103` (Service1 secondary, TargetCompID=BROKER2), `:15102` (QuikFIXAdapterService2, TargetCompID=TEST). TCP reachable from Gateway host. |
| Production host + port | [ ] | |
| TLS required? | [ ] | Version, ciphers, mTLS |
| IP allowlisting for Gateway egress | [ ] | |
| Firewall / VPN requirements | [ ] | |
| Max concurrent sessions per adapter license | [ ] | |

## 4. Application messages

Confirm support and field requirements for:

| MsgType | Name | Status |
|---------|------|--------|
| D | NewOrderSingle | [ ] |
| F | OrderCancelRequest | [ ] |
| G | OrderCancelReplaceRequest | [ ] |
| 8 | ExecutionReport | [ ] |
| 9 | OrderCancelReject | [ ] |
| j | BusinessMessageReject | [ ] |
| 3 | Reject | [ ] |

For each message: required tags, conditional tags, max lengths, enums, repeating groups (Parties, etc.).

## 5. Order identification & idempotency

| Item | Status | Notes |
|------|--------|-------|
| ClOrdID uniqueness scope | [ ] | Per session / per day / forever |
| ClOrdID max length & charset | [ ] | |
| Behavior on duplicate ClOrdID | [ ] | Reject vs ignore vs return previous |
| OrderID (tag 37) assignment rules | [ ] | QUIK order number mapping |
| SecondaryOrderID / ExecID semantics | [ ] | |
| OrigClOrdID rules for cancel/replace | [ ] | |

## 6. Instrument & account mapping

| Item | Status | Notes |
|------|--------|-------|
| Symbol vs SecurityID usage | [x] | NewOrder: only **48** SecurityID + **22=8**; no tag 55 |
| SecurityExchange / Board tag | [x] | Class code = ExDestination **100** (not 207); sample `LISTEDSTOCKK` |
| Account (tag 1) format | [ ] | |
| ClientID / FirmID / PartyID mapping to QUIK | [ ] | |
| Currency / SettlType requirements | [ ] | |

## 7. Execution reports & status mapping

| Item | Status | Notes |
|------|--------|-------|
| ExecType / OrdStatus matrix | [ ] | NEW, PARTIAL, FILL, CANCELED, REJECTED, EXPIRED, REPLACE, RESTATE |
| Partial fill aggregation rules | [ ] | |
| AvgPx / LastPx / CumQty / LeavesQty precision | [ ] | |
| Text for unsolicited cancels / busts | [ ] | |
| RESTATE after NEW (ARQA 4.4+) | [ ] | |

## 8. Health / connectivity diagnostics

| Item | Status | Notes |
|------|--------|-------|
| How to detect TCP up vs FIX logged on | [~] | QuickFIX session state |
| How to detect QUIK link down while FIX session up | [ ] | ARQA 3.71 connection binding / logout reason |
| Logout / Reject texts for QUIK disconnect | [ ] | |
| Recommended reconnect policy | [ ] | |

## 9. Test environment

| Item | Status | Notes |
|------|--------|-------|
| Trial / UAT credentials for ≥2 sessions | [~] | CompIDs known (`TEST`, `BROKER2`). QuikApi user on adapter host is for ARQA↔QUIK only — not FIX Logon Username/Password unless ARQA requires tags 553/554 |
| Sample FIX captures: NewOrder → NEW → FILL | [ ] | |
| Sample: reject, cancel, replace, partial fill | [ ] | |
| Contact for integration support | [ ] | |
| Quiet hours / test calendar | [ ] | |

## 10. Non-functional

| Item | Status | Notes |
|------|--------|-------|
| Throughput limits (orders/day or /sec) | [ ] | License tiers exist |
| Max accounts per adapter | [ ] | |
| Latency SLO expectations | [ ] | |
| Logging / audit requirements from ARQA side | [ ] | |

---

## Gateway readiness gate

Production ARQA wiring is **blocked** until items in sections 1–5 and 9 are received (at least for UAT).

Until then:

- Local `fixsimulator` provides e2e coverage.
- `internal/adapters/arqa` is implemented against [`arqa-fix-contract.md`](./arqa-fix-contract.md) assumptions and reconfigured when official specs arrive.

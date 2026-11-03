-- Disposable stand-in for QUIK quik_export2 on the isolated local stack only.
-- fixsimulator writes "Orders" as QUIK would; the Gateway reads it with a
-- SELECT-only role, the same privilege it should have on the real source.
-- "Params" (current trades) holds a few static rows; the real table's columns
-- are defined by QUIK and passed through unchanged by the Gateway.
\set ON_ERROR_STOP 1
SELECT 'CREATE DATABASE quik_export_local'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'quik_export_local')\gexec

DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'quik_sim_writer') THEN
    CREATE ROLE quik_sim_writer LOGIN PASSWORD 'local-simulator-only';
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'quik_reader') THEN
    CREATE ROLE quik_reader LOGIN PASSWORD 'local-simulator-only';
  END IF;
END $$;

\connect quik_export_local
-- Columns follow the shape used by the QUIK Orders integration tests.
CREATE TABLE IF NOT EXISTS public."Orders" (
  "OrderNum" numeric(19,0) NOT NULL,
  "ClassCode" text NOT NULL,
  "TradeDate" timestamp NOT NULL,
  "SecCode" text,
  "FirmId" text,
  "ClientCode" text,
  "Account" text,
  "Operation" text,
  "State" text,
  "Qty" numeric,
  "Balance" numeric,
  "Price" numeric,
  PRIMARY KEY ("OrderNum", "ClassCode", "TradeDate")
);
CREATE TABLE IF NOT EXISTS public."Params" (
  "ClassCode" text NOT NULL,
  "SecCode" text NOT NULL,
  "SecName" text,
  "TradeDate" date,
  "status" text,
  "Bid" numeric,
  "Offer" numeric,
  "Last" numeric,
  "Open" numeric,
  "High" numeric,
  "Low" numeric,
  "NumTrades" numeric,
  "VolToday" numeric,
  "ValToday" numeric,
  PRIMARY KEY ("ClassCode", "SecCode")
);
INSERT INTO public."Params" VALUES
  ('LIST_GBA', 'GBA02280810', 'Local simulator bond', current_date, 'торгуется', 99.5, 100.5, 100, 99.5, 101, 99, 3, 1500, 150000),
  ('LISTEDSTOCKK', 'KG0105166315', 'Local simulator share', current_date, 'торгуется', NULL, 400, 400, 400, 400, 400, 1, 1, 400)
ON CONFLICT DO NOTHING;
-- The columns the Gateway selects, named as in the real QUIK table.
ALTER TABLE public."Params"
  ADD COLUMN IF NOT EXISTS lotsize numeric, ADD COLUMN IF NOT EXISTS tradingstatus text,
  ADD COLUMN IF NOT EXISTS pricemin numeric, ADD COLUMN IF NOT EXISTS pricemax numeric, ADD COLUMN IF NOT EXISTS realminstep numeric,
  ADD COLUMN IF NOT EXISTS bid numeric, ADD COLUMN IF NOT EXISTS biddepth numeric, ADD COLUMN IF NOT EXISTS "offer" numeric, ADD COLUMN IF NOT EXISTS offerdepth numeric,
  ADD COLUMN IF NOT EXISTS "last" numeric, ADD COLUMN IF NOT EXISTS qty numeric, ADD COLUMN IF NOT EXISTS "time" timestamp,
  ADD COLUMN IF NOT EXISTS "open" numeric, ADD COLUMN IF NOT EXISTS high numeric, ADD COLUMN IF NOT EXISTS low numeric,
  ADD COLUMN IF NOT EXISTS prevprice numeric, ADD COLUMN IF NOT EXISTS waprice numeric, ADD COLUMN IF NOT EXISTS numtrades numeric,
  ADD COLUMN IF NOT EXISTS voltoday numeric, ADD COLUMN IF NOT EXISTS valtoday numeric,
  ADD COLUMN IF NOT EXISTS accruedint numeric, ADD COLUMN IF NOT EXISTS yield numeric, ADD COLUMN IF NOT EXISTS nextcoupon timestamp, ADD COLUMN IF NOT EXISTS couponperiod numeric;
UPDATE public."Params" SET lotsize = 1, tradingstatus = 'открыта', bid = "Bid", "offer" = "Offer", "last" = "Last", "open" = "Open", high = "High", low = "Low",
  numtrades = "NumTrades", voltoday = "VolToday", valtoday = "ValToday";
-- Instrument and class names that the Gateway joins to Params rows. The
-- local classes are not LIST_GBA: choose them on the "Текущие торги" page.
CREATE TABLE IF NOT EXISTS public."Securities" (
  "TradeDate" timestamp NOT NULL, "ClassCode" text NOT NULL, "SecCode" text NOT NULL,
  "CouponValue" numeric, "FaceUnit" text, "SecShortName" text, "SecFullName" text,
  PRIMARY KEY ("TradeDate", "ClassCode", "SecCode")
);
INSERT INTO public."Securities" VALUES
  (current_date, 'LIST_GBA', 'GBA02280810', 2.5, 'KGS', 'Local bond', 'Local simulator bond'),
  (current_date, 'LISTEDSTOCKK', 'KG0105166315', 0, 'KGS', 'Local share', 'Local simulator share'),
  -- What the local scripts trade; new orders are checked against this table.
  (current_date, 'TQBR', 'SBER', 0, 'RUB', 'Sber', 'Local simulator Sber'),
  (current_date, 'TQBR', 'GAZP', 0, 'RUB', 'Gazprom', 'Local simulator Gazprom'),
  (current_date, 'TQBR', 'REJECT', 0, 'RUB', 'Reject', 'Simulator rejects this instrument'),
  (current_date, 'TQBR', 'FAULT_RECOVER', 0, 'RUB', 'Fault', 'Simulator fault recovery instrument')
ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS public."Classes" (
  "TradeDate" timestamp NOT NULL, "ClassCode" text NOT NULL, "ClassName" text,
  PRIMARY KEY ("TradeDate", "ClassCode")
);
INSERT INTO public."Classes" VALUES
  (current_date, 'LIST_GBA', 'Local bonds'),
  (current_date, 'LISTEDSTOCKK', 'Local shares')
ON CONFLICT DO NOTHING;
-- The simulator does not write trades; static rows show both sides of one trade.
CREATE TABLE IF NOT EXISTS public."Trades" (
  "TradeNum" numeric(19,0) NOT NULL, "ClassCode" text NOT NULL, "TradeDate" timestamp NOT NULL, "SecCode" text, "OrderNum" numeric(19,0),
  "FirmId" text NOT NULL, "Account" text NOT NULL, "ClientCode" text, "Operation" text NOT NULL, "Qty" numeric, "Price" numeric,
  PRIMARY KEY ("TradeNum", "ClassCode", "TradeDate", "Operation", "FirmId", "Account")
);
INSERT INTO public."Trades" VALUES
  (1, 'TQBR', current_date, 'SBER', 1, 'SIM', 'SMOKE', 'SMOKE', 'Купля', 1, 100),
  (1, 'TQBR', current_date, 'SBER', 2, 'LOCAL_FIRM', 'LOCAL_ACCOUNT', 'DEMO', 'Продажа', 1, 100)
ON CONFLICT DO NOTHING;
-- Static positions; the simulator does not change them on fills.
CREATE TABLE IF NOT EXISTS public."MoneyLimits" (
  "LoadDate" timestamp NOT NULL, "FirmId" text NOT NULL, "ClientCode" text NOT NULL, "Tag" text NOT NULL, "CurrCode" text NOT NULL, "LimitKind" text NOT NULL,
  "CurrentBal" numeric, "CurrentLimit" numeric, "Locked" numeric, "OpenBal" numeric, "OpenLimit" numeric,
  PRIMARY KEY ("LoadDate", "FirmId", "ClientCode", "Tag", "CurrCode", "LimitKind")
);
INSERT INTO public."MoneyLimits" VALUES
  (current_date, 'SIM', 'SMOKE', 'EQTV', 'RUB', 'T0', 1000000, 0, 0, 1000000, 0),
  (current_date, 'LOCAL_FIRM', 'DEMO', 'EQTV', 'KGS', 'T0', 500000.5, 0, 0, 500000.5, 0)
ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS public."DepoLimits" (
  "LoadDate" timestamp NOT NULL, "FirmId" text NOT NULL, "ClientCode" text NOT NULL, "SecCode" text NOT NULL, "Trdacc" text NOT NULL, "LimitKind" text NOT NULL,
  "CurrentBal" numeric, "CurrentLimit" numeric, "OpenBal" numeric, "OpenLimit" numeric, "AwgPositionPrice" numeric,
  PRIMARY KEY ("LoadDate", "FirmId", "ClientCode", "SecCode", "Trdacc", "LimitKind")
);
INSERT INTO public."DepoLimits" VALUES
  (current_date, 'SIM', 'SMOKE', 'SBER', 'SMOKE', 'T0', 10, 0, 10, 0, 100),
  (current_date, 'LOCAL_FIRM', 'DEMO', 'GAZP', 'LOCAL_ACCOUNT', 'T0', 25, 0, 25, 0, 150)
ON CONFLICT DO NOTHING;
-- Trading accounts and firms the local scripts trade with (QUIK column names).
CREATE TABLE IF NOT EXISTS public."Firms" (
  "Exchange" text, "FirmId" text NOT NULL, "FirmName" text, "Status" text, "StatusFlag" integer,
  PRIMARY KEY ("FirmId")
);
INSERT INTO public."Firms" VALUES
  ('LOCAL', 'SIM', 'Local simulator firm', NULL, NULL),
  ('LOCAL', 'LOCAL_FIRM', 'Local demo firm', NULL, NULL)
ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS public."Trdaccs" (
  "TrdAccId" text NOT NULL, "Type" integer, "FirmId" text NOT NULL, "LoadDate" timestamp, "Description" text,
  "BankAccId" text, "Status" integer, "DepAccId" text, "TrdAccType" integer, "MainTrdAccId" text,
  "FirmUse" integer, "FullCoveredSell" integer, "BankIdT0" text, "BankIdTPlus" text, "DepUnitId" text, "Flags" bigint,
  PRIMARY KEY ("FirmId", "TrdAccId")
);
INSERT INTO public."Trdaccs" ("TrdAccId", "FirmId", "Description")
SELECT account, firm, 'Local simulator account'
FROM (VALUES ('SMOKE', 'SIM'), ('RELIABILITY', 'SIM'), ('RECOVERY', 'SIM'), ('CAPACITY', 'SIM'), ('VERIFY', 'SIM'),
             ('LOCAL_ACCOUNT', 'LOCAL_FIRM')) AS v(account, firm)
ON CONFLICT DO NOTHING;
REVOKE ALL ON public."Orders" FROM PUBLIC;
REVOKE ALL ON public."Firms", public."Trdaccs" FROM PUBLIC;
REVOKE ALL ON public."Params", public."Securities", public."Classes", public."Trades", public."MoneyLimits", public."DepoLimits" FROM PUBLIC;
GRANT CONNECT ON DATABASE quik_export_local TO quik_sim_writer, quik_reader;
GRANT USAGE ON SCHEMA public TO quik_sim_writer, quik_reader;
GRANT SELECT, INSERT, UPDATE ON public."Orders" TO quik_sim_writer;
GRANT SELECT ON public."Orders", public."Params", public."Securities", public."Classes", public."Trades", public."MoneyLimits", public."DepoLimits", public."Firms", public."Trdaccs" TO quik_reader;

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
  "Status" text,
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
REVOKE ALL ON public."Orders" FROM PUBLIC;
REVOKE ALL ON public."Params" FROM PUBLIC;
GRANT CONNECT ON DATABASE quik_export_local TO quik_sim_writer, quik_reader;
GRANT USAGE ON SCHEMA public TO quik_sim_writer, quik_reader;
GRANT SELECT, INSERT, UPDATE ON public."Orders" TO quik_sim_writer;
GRANT SELECT ON public."Orders", public."Params" TO quik_reader;

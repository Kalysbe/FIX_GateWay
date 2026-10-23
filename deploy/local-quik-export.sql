-- Disposable stand-in for QUIK quik_export2 on the isolated local stack only.
-- fixsimulator writes "Orders" as QUIK would; the Gateway reads it with a
-- SELECT-only role, the same privilege it should have on the real source.
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
REVOKE ALL ON public."Orders" FROM PUBLIC;
GRANT CONNECT ON DATABASE quik_export_local TO quik_sim_writer, quik_reader;
GRANT USAGE ON SCHEMA public TO quik_sim_writer, quik_reader;
GRANT SELECT, INSERT, UPDATE ON public."Orders" TO quik_sim_writer;
GRANT SELECT ON public."Orders" TO quik_reader;

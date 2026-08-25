-- QUIK export (quik_export2): Quotes(symbol)
-- emitents.symbol → emitents.code → "Orders"."SecCode"[7:10]
-- plus SecShortName / SecFullName from "Securities"
--
-- Usage:
--   SELECT * FROM public."Quotes"('MAIR');

CREATE INDEX IF NOT EXISTS idx_emitents_symbol_upper
    ON public.emitents (upper(btrim(symbol)))
    WHERE symbol IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_orders_seccode_chars7_10
    ON public."Orders" (substring("SecCode" FROM 7 FOR 4));

DROP FUNCTION IF EXISTS public.orders_by_emitent_symbol(text);
DROP FUNCTION IF EXISTS public."Quotes"(text);

CREATE OR REPLACE FUNCTION public."Quotes"(p_symbol text)
RETURNS TABLE (
    "OrderNum" numeric(19,0),
    "ClassCode" character varying(12),
    "TradeDate" timestamp without time zone,
    "SecCode" character varying(12),
    "Accruedint" numeric(19,8),
    "Price" numeric(19,8),
    "Value" numeric(28,8),
    "Yield" numeric(19,4),
    "Price2" numeric(19,8),
    "Balance" numeric(28,8),
    "Balance_pcs" numeric(28,8),
    "OrderDateTime" timestamp without time zone,
    "OrderTimeMs" integer,
    "Qty" numeric(28,8),
    "Qty_pcs" numeric(28,8),
    "UID" integer,
    "WithdrawDateTime" timestamp without time zone,
    "WithdrawTimeMs" integer,
    "Account" character varying(12),
    "BrokerRef" character varying(34),
    "ClientCode" character varying(12),
    "UserId" character varying(32),
    "SettleCode" character varying(12),
    "Operation" character varying(40),
    "State" character varying(12),
    "Type" character varying(12),
    "FirmId" character varying(12),
    "FirmName" character varying(128),
    "ExchangeCode" character varying(64),
    "ActivationTime" timestamp without time zone,
    "LinkedOrder" numeric(19,0),
    "Expiry" character varying(20),
    "IsMarketMaker" character varying(8),
    "TransId" numeric(10,0),
    "Visible" numeric(28,8),
    "Period" integer,
    "Flags" integer,
    "BankAccId" character varying(12),
    "ValueEntryType" character varying(16),
    "RepoTerm" integer,
    "RepoValue" numeric(19,8),
    "Repo2Value" numeric(19,8),
    "RepoValueBalance" numeric(19,8),
    "StartDiscount" numeric(19,8),
    "RejectReason" character varying(128),
    "ExtOrderFlags" integer,
    "MinQty" numeric(28,8),
    "ExecType" character varying(32),
    "SideQualifier" smallint,
    "AcntType" smallint,
    "Capacity" smallint,
    "PassiveOnlyOrder" smallint,
    "StopOrder" numeric(19,0),
    "AwgPrice" numeric(19,8),
    "CanceledUserId" numeric(10,0),
    "ExpiryTime" timestamp without time zone,
    "PriceCurrency" character varying(4),
    "SettleCurrency" character varying(4),
    "AcceptedUID" numeric(10,0),
    "ExtOrderStatus" integer,
    "FilledValue" numeric(19,8),
    "Extref" character varying(32),
    "OnBehalfOfUID" numeric(10,0),
    "ClientQualifier" integer,
    "ClientShortCode" numeric(19,0),
    "InvestmentDecisionMakerQualifier" integer,
    "InvestmentDecisionMakerShortCode" numeric(19,0),
    "ExecutingTraderQualifier" integer,
    "ExecutingTraderShortCode" numeric(19,0),
    "SettleDate" timestamp without time zone,
    "SettleDate2" timestamp without time zone,
    "StartDate" timestamp without time zone,
    "OperationType" integer,
    "VisibilityFactor" numeric(19,2),
    "VisibilityFactorScale" integer,
    "VisibleRepoValue" numeric(19,8),
    "TradingSession" integer,
    "PriceEntryType" integer,
    "Qty2" numeric(28,8),
    "Value2" numeric(19,8),
    "LSecCode" character varying(12),
    "Benchmark" character varying(12),
    "FirstCurrCode" character varying(4),
    "FirstCurrQty" numeric(28,8),
    "SecondCurrCode" character varying(4),
    "SecondCurrQty" numeric(28,8),
    "ExternalQty" numeric(28,8),
    "SecShortName" character varying(32),
    "SecFullName" character varying(128)
)
LANGUAGE plpgsql
STABLE
AS $$
DECLARE
    v_code text;
BEGIN
    IF p_symbol IS NULL OR char_length(btrim(p_symbol)) <> 4 THEN
        RAISE EXCEPTION 'symbol must be exactly 4 characters'
            USING ERRCODE = '22023';
    END IF;

    SELECT btrim(e.code::text)
    INTO v_code
    FROM public.emitents e
    WHERE upper(btrim(e.symbol)) = upper(btrim(p_symbol))
    ORDER BY e.id
    LIMIT 1;

    IF v_code IS NULL OR v_code = '' THEN
        RETURN;
    END IF;

    RETURN QUERY
    SELECT
        o."OrderNum",
        o."ClassCode",
        o."TradeDate",
        o."SecCode",
        o."Accruedint",
        o."Price",
        o."Value",
        o."Yield",
        o."Price2",
        o."Balance",
        o."Balance_pcs",
        o."OrderDateTime",
        o."OrderTimeMs",
        o."Qty",
        o."Qty_pcs",
        o."UID",
        o."WithdrawDateTime",
        o."WithdrawTimeMs",
        o."Account",
        o."BrokerRef",
        o."ClientCode",
        o."UserId",
        o."SettleCode",
        o."Operation",
        o."State",
        o."Type",
        o."FirmId",
        o."FirmName",
        o."ExchangeCode",
        o."ActivationTime",
        o."LinkedOrder",
        o."Expiry",
        o."IsMarketMaker",
        o."TransId",
        o."Visible",
        o."Period",
        o."Flags",
        o."BankAccId",
        o."ValueEntryType",
        o."RepoTerm",
        o."RepoValue",
        o."Repo2Value",
        o."RepoValueBalance",
        o."StartDiscount",
        o."RejectReason",
        o."ExtOrderFlags",
        o."MinQty",
        o."ExecType",
        o."SideQualifier",
        o."AcntType",
        o."Capacity",
        o."PassiveOnlyOrder",
        o."StopOrder",
        o."AwgPrice",
        o."CanceledUserId",
        o."ExpiryTime",
        o."PriceCurrency",
        o."SettleCurrency",
        o."AcceptedUID",
        o."ExtOrderStatus",
        o."FilledValue",
        o."Extref",
        o."OnBehalfOfUID",
        o."ClientQualifier",
        o."ClientShortCode",
        o."InvestmentDecisionMakerQualifier",
        o."InvestmentDecisionMakerShortCode",
        o."ExecutingTraderQualifier",
        o."ExecutingTraderShortCode",
        o."SettleDate",
        o."SettleDate2",
        o."StartDate",
        o."OperationType",
        o."VisibilityFactor",
        o."VisibilityFactorScale",
        o."VisibleRepoValue",
        o."TradingSession",
        o."PriceEntryType",
        o."Qty2",
        o."Value2",
        o."LSecCode",
        o."Benchmark",
        o."FirstCurrCode",
        o."FirstCurrQty",
        o."SecondCurrCode",
        o."SecondCurrQty",
        o."ExternalQty",
        s."SecShortName",
        s."SecFullName"
    FROM public."Orders" o
    LEFT JOIN public."Securities" s
      ON s."SecCode" = o."SecCode"
     AND s."ClassCode" = o."ClassCode"
    WHERE substring(o."SecCode" FROM 7 FOR 4) = v_code;
END;
$$;

COMMENT ON FUNCTION public."Quotes"(text) IS
    'Looks up emitents by 4-char symbol, takes code, returns Orders (SecCode chars 7-10 = code) plus Securities.SecShortName and SecFullName. Call as SELECT * FROM public."Quotes"(''MAIR'');';

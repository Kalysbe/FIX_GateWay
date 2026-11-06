//go:build integration

package quikexport

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMarketTradesOncePerTradeChronological(t *testing.T) {
	dsn := os.Getenv("QUIK_ORDERS_STORE_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("QUIK_ORDERS_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("disposable QUIK_ORDERS_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var name string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil || !strings.HasSuffix(name, "_test") {
		t.Fatal("disposable database name must end in _test", name, err)
	}
	// The QUIK server's own zone is not the exchange's; output must not depend on it.
	_, err = pool.Exec(ctx, `SET TimeZone='Asia/Almaty';
CREATE TABLE public."Trades" ("TradeNum" numeric,"Operation" varchar,"ClassCode" varchar,"TradeDate" timestamp,"SecCode" varchar,"Kind" varchar,
"Price" numeric,"Qty" numeric,"TradeDateTime" timestamp,"TradeTimeMs" integer,"FirmId" varchar,"CPFirmId" varchar,"Account" varchar,
"SettleCurrency" varchar,"CanceledDateTime" timestamp,"CanceledDateTimeMs" integer);
INSERT INTO public."Trades" VALUES
(12,'Купля','LIST_GBA','2026-11-05','GBA1','Обычная',99.00000000,3,'2026-11-05 09:00:00',NULL,'2040','BAKAI','A','KGS',NULL,NULL),
(12,'Продажа','LIST_GBA','2026-11-05','GBA1','Обычная',99.00000000,3,'2026-11-05 09:00:00',NULL,'BAKAI','2040','B','KGS',NULL,NULL),
(11,'Купля','LIST_GBA','2026-11-05','GBA1','Обычная',98,1,'2026-11-05 10:41:45',250,'2040','BAKAI','A','KGS','2026-11-05 11:00:00',NULL),
(11,'Продажа','LIST_GBA','2026-11-05','GBA1','Обычная',98,1,'2026-11-05 10:41:45',250,'BAKAI','2040','B','KGS','2026-11-05 11:00:00',NULL),
(13,'Купля','LIST_GBA','2026-11-04','GBA1','Обычная',97,1,'2026-11-04 15:00:00',NULL,'2040','BAKAI','A','KGS',NULL,NULL),
(14,'Купля','REPO','2026-11-05','GBA1','Обычная',1,1,'2026-11-05 09:30:00',NULL,'2040','BAKAI','A','KGS',NULL,NULL),
(15,'Купля','LIST_GD','2026-11-05','GD1','Обычная',5,2,'2026-11-05 09:15:00',NULL,'2040','BAKAI','A','RUB',NULL,NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE public."Trades"`) })
	s := NewStore(pool)
	s.SetServedClasses(func(context.Context) ([]string, error) { return []string{"LIST_GBA", "LIST_GD"}, nil })

	page, err := s.ListMarketTrades(ctx, MarketTradeFilter{Limit: 10})
	if err != nil || len(page.Trades) != 4 {
		t.Fatal("one row per trade, served classes only", page, err)
	}
	var nums []string
	for _, tr := range page.Trades {
		nums = append(nums, tr.Text("TradeNum"))
	}
	if strings.Join(nums, ",") != "13,12,15,11" {
		t.Fatal("chronological by TradeDateTime", nums)
	}
	if got := string(page.Trades[3].Raw); got != `{"TradeNum":11,"ClassCode":"LIST_GBA","SecCode":"GBA1","TradeDateTime":"2026-11-05T10:41:45.250+06:00","Price":98,"Qty":1,"SettleCurrency":"KGS","Kind":"Обычная","CanceledDateTime":"2026-11-05T11:00:00.000+06:00"}` {
		t.Fatal("fields, KSE offset, cancellation", got)
	}
	if strings.Contains(string(page.Trades[1].Raw), "2040") || !strings.Contains(string(page.Trades[1].Raw), `"Price":99.00000000`) {
		t.Fatal("no participants, values as stored", string(page.Trades[1].Raw))
	}
	from := time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC) // 09:00 KSE, inclusive
	before := time.Date(2026, 11, 5, 9, 15, 0, 0, KSETime)
	page, err = s.ListMarketTrades(ctx, MarketTradeFilter{ClassCode: "LIST_GBA", SecCode: "GBA1", From: &from, Before: &before, Limit: 1})
	if err != nil || len(page.Trades) != 1 || page.Trades[0].Text("TradeNum") != "12" || page.HasMore {
		t.Fatal("instrument and period", page, err)
	}
	page, err = s.ListMarketTrades(ctx, MarketTradeFilter{Limit: 2, Offset: 2})
	if err != nil || len(page.Trades) != 2 || page.HasMore || page.NextOffset != 4 || page.Trades[0].Text("TradeNum") != "15" {
		t.Fatal("paging", page, err)
	}
}

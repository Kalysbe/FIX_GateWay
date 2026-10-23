package main

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Optional QUIK export emulation for the disposable local stack: real QUIK
// publishes its order book to quik_export2.public."Orders", which the Gateway
// reads as the only source of current orders. With SIM_ORDERS_DATABASE_URL set,
// the simulator keeps an equivalent table in step with its own FIX replies.
// Rejected transactions get no row, as in QUIK.
var ordersExport *pgxpool.Pool

func openOrdersExport(url string) {
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		log.Printf("orders export disabled: %v", err)
		return
	}
	ordersExport = pool
	log.Printf("orders export enabled")
}

type exportRow struct {
	number, board, tradeDate, symbol, firm, client, account string
	operation, state, qty, balance, price                    string
}

func newExportRow(f map[string]string, number, board string, now time.Time) *exportRow {
	operation := "Купля"
	if f["54"] == "2" {
		operation = "Продажа"
	}
	qty := firstNonEmptySim(f["38"], "1")
	return &exportRow{
		number: number, board: board, tradeDate: now.UTC().Format("2006-01-02"),
		symbol: firstNonEmptySim(f["48"], f["55"]), firm: f["5018"], client: f["109"], account: f["1"],
		operation: operation, state: "Активна", qty: qty, balance: qty, price: firstNonEmptySim(f["44"], "0"),
	}
}

// writeExport runs before the matching FIX report is sent, so a client that
// sees the report can already read the same state from Orders.
func writeExport(row *exportRow) {
	if ordersExport == nil || row == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := ordersExport.Exec(ctx, `INSERT INTO public."Orders"
("OrderNum","ClassCode","TradeDate","SecCode","FirmId","ClientCode","Account","Operation","State","Qty","Balance","Price")
VALUES ($1::numeric,$2,$3::date,$4,$5,$6,$7,$8,$9,$10::numeric,$11::numeric,$12::numeric)
ON CONFLICT ("OrderNum","ClassCode","TradeDate") DO UPDATE SET "State"=EXCLUDED."State","Balance"=EXCLUDED."Balance"`,
		row.number, row.board, row.tradeDate, row.symbol, row.firm, row.client, row.account,
		row.operation, row.state, row.qty, row.balance, row.price)
	if err != nil {
		log.Printf("orders export write failed for %s: %v", row.number, err)
	}
}

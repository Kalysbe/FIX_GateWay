package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/tradeledger"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	file := flag.String("quik-file", "", "saved read-only QUIK /api/trades response")
	backfill := flag.Bool("backfill", true, "restore ledger from persisted FIX reports")
	applyMigrations := flag.Bool("migrate", false, "apply forward migrations before reconciliation")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if *applyMigrations {
		if err := migrate.Up(os.Getenv("DATABASE_URL"), "migrations"); err != nil {
			fail(err)
		}
	}
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	store := tradeledger.NewStore(pool)
	if *backfill {
		n, bad, err := store.Backfill(ctx)
		if err != nil {
			fail(err)
		}
		fmt.Printf("FIX source reports: %d; errors: %d\n", n, bad)
	}
	if *file != "" {
		f, err := os.Open(*file)
		if err != nil {
			fail(err)
		}
		defer f.Close()
		id, err := store.StartRun(ctx, "file:"+*file)
		if err != nil {
			fail(err)
		}
		n, bad, problem := store.Import(ctx, f)
		if err := store.FinishRun(ctx, id, n, bad, problem); err != nil {
			fail(err)
		}
		if problem != nil {
			fail(problem)
		}
		fmt.Printf("QUIK source sides: %d\n", n)
	}
	coverage, err := store.Coverage(ctx)
	if err != nil {
		fail(err)
	}
	var pretty any
	_ = json.Unmarshal(coverage, &pretty)
	out, _ := json.MarshalIndent(pretty, "", "  ")
	fmt.Println(string(out))
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

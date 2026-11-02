package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/fix-gateway/backend/internal/platform/db"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/quikexport"
	"github.com/fix-gateway/backend/internal/tradeledger"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	quik := flag.Bool("quik", false, `reconcile with QUIK public."Trades" over read-only QUIK_EXPORT_DATABASE_URL`)
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
	store.SetClasses(quikexport.NewClassSettings(pool).Classes)
	if *quik {
		quikPool, err := db.OpenReadOnly(ctx, os.Getenv("QUIK_EXPORT_DATABASE_URL"))
		if err != nil {
			fail(err)
		}
		defer quikPool.Close()
		n, problem := store.Sync(ctx, quikexport.NewStore(quikPool))
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

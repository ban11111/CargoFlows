package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"cargoflows/api/internal/config"
	"cargoflows/api/internal/database"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	if err := database.Migrate(db.WithContext(ctx)); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	log.Print("database migration completed")
}

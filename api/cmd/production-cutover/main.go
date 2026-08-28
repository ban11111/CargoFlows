package main

import (
	"context"
	"log"
	"time"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/config"
	"cargoflows/api/internal/database"
)

func main() {
	cfg := config.Load()
	db, err := database.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	result, err := ai.CancelUnfinishedForProduction(context.Background(), db, time.Now().UTC())
	if err != nil {
		log.Fatalf("cancel unfinished AI work: %v", err)
	}
	log.Printf("production cutover completed: jobs=%d items=%d executions=%d image_turns=%d", result.Jobs, result.Items, result.Executions, result.ImageTurns)
}

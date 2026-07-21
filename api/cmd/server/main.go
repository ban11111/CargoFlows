package main

import (
	"context"
	"log"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/app"
	"cargoflows/api/internal/config"
	"cargoflows/api/internal/database"
)

func main() {
	cfg := config.Load()

	db, err := database.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	if err := database.Seed(db); err != nil {
		log.Fatalf("seed database: %v", err)
	}

	router := app.NewRouter(cfg, db)
	if costs, err := app.NewOpenAICostService(cfg, db); err != nil {
		log.Fatalf("configure OpenAI cost synchronization: %v", err)
	} else if costs != nil {
		go ai.RunDailyCostSync(context.Background(), costs, func(err error) { log.Printf("OpenAI cost synchronization failed: %v", err) })
	}
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("run server: %v", err)
	}
}

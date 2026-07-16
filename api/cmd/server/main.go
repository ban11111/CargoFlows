package main

import (
	"log"

	"cargoflow/api/internal/app"
	"cargoflow/api/internal/config"
	"cargoflow/api/internal/database"
)

func main() {
	cfg := config.Load()

	db, err := database.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	if err := database.Migrate(db); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	if err := database.Seed(db); err != nil {
		log.Fatalf("seed database: %v", err)
	}

	router := app.NewRouter(cfg, db)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("run server: %v", err)
	}
}

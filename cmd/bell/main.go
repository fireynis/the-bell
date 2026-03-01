package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/database"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()
	log.Println("database connected")

	if err := database.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("running migrations: %v", err)
	}
	log.Println("migrations complete")

	log.Println("the-bell: ready, waiting for signal")
	<-ctx.Done()
	log.Println("the-bell: shutting down")
}

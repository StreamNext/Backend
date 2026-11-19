package main

import (
	"context"
	"log"

	"github.com/StreamNext/Backend/internal/config"
	"github.com/StreamNext/Backend/internal/db"
)

func main() {
	dbURL, err := config.DatabaseURL()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dbURL, "streamnext-migrate")
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/luiz504/week-tech-go-server/internal/api"
	"github.com/luiz504/week-tech-go-server/internal/store/pg"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Error loading .env file 💥: %v", err)
	}
	ctx := context.Background()

	poll, err := pgxpool.New(ctx, fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s",
		os.Getenv("WS_DATABASE_HOST"),
		os.Getenv("WS_DATABASE_PORT"),
		os.Getenv("WS_DATABASE_USER"),
		os.Getenv("WS_DATABASE_PASSWORD"),
		os.Getenv("WS_DATABASE_NAME"),
	))

	if err != nil {
		log.Fatalf("Error connecting to database 💥: %v", err)
	}

	defer poll.Close()

	if err := poll.Ping(ctx); err != nil {
		log.Fatalf("Error pinging database 💥: %v", err)
	}

	handler := api.NewHandler(pg.New(poll))

	port := "8080"
	address := fmt.Sprintf(":%s", port)

	go func() {
		log.Printf("Server is starting on http:localhost:%s", port)
		if err := http.ListenAndServe(address, handler); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("Error starting server 💥: %v", err)
			}
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	log.Println("Shutting down server...")
}

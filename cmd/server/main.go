package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/chris/event_watch/internal/server"
)

func main() {
	cfg, err := server.LoadConfig(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := server.Run(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}

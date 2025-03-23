package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/No3371/dc_embed_throttler/bot"
	"github.com/No3371/dc_embed_throttler/config"
	"github.com/No3371/dc_embed_throttler/storage"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize storage
	store, err := storage.NewSQLiteStorage(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()
	ctx := contextWithSigterm(context.Background())

	// Create and start bot
	b, err := bot.NewBot(cfg, store)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	log.Println("Starting bot...")
	if err := b.Start(ctx); err != nil {
		log.Fatalf("Failed to start bot: %v", err)
	}

	<-ctx.Done()
}

// https://gist.github.com/matejb/87064825093c42c1e76e7175665d9a9b
func contextWithSigterm(ctx context.Context) context.Context {
	ctxWithCancel, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()

		signalCh := make(chan os.Signal, 1)
		signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)

		select {
		case <-signalCh:
		case <-ctx.Done():
		}
	}()

	return ctxWithCancel
}

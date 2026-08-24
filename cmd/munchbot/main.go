// Command munchbot runs the MunchBot Discord application: a ranked-choice
// lunch-decision bot backed by Postgres.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/stockbauer/munchbot/internal/bot"
	"github.com/stockbauer/munchbot/internal/config"
	"github.com/stockbauer/munchbot/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return err
	}

	b, err := bot.New(cfg.DiscordToken, st, cfg.GuildID)
	if err != nil {
		return err
	}
	if err := b.Start(); err != nil {
		return err
	}
	defer b.Stop()

	go b.RunScheduler(ctx)

	log.Println("munchbot: up and running")
	<-ctx.Done()
	log.Println("munchbot: shutting down")
	return nil
}

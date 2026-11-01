package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	_ "time/tzdata" // guild timezones must not depend on the image having zoneinfo

	"github.com/happyx-app/HappyX.Bot/internal/api"
	"github.com/happyx-app/HappyX.Bot/internal/bot"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// Configuration comes only from the environment; see .env.example.
func run(log *slog.Logger) error {
	token := os.Getenv("DISCORD_TOKEN")
	apiURL := strings.TrimRight(os.Getenv("API_URL"), "/")
	apiKey := os.Getenv("API_KEY")
	if token == "" || apiURL == "" || apiKey == "" {
		return errors.New("DISCORD_TOKEN, API_URL and API_KEY are required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	b, err := bot.New(token, api.New(apiURL, apiKey), log)
	if err != nil {
		return err
	}
	b.SetWebURL(os.Getenv("WEB_URL")) // optional: links to the game and the privacy policy on the dashboard
	return b.Run(ctx)
}

// Package config loads MunchBot's runtime configuration from the environment.
package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds everything MunchBot needs to connect to Discord and Postgres.
type Config struct {
	DiscordToken string
	DatabaseURL  string
	GuildID      string
}

// Load reads configuration from environment variables, first loading a
// local .env file (if present) into the process environment. Real
// environment variables always take precedence over .env, so deployments
// that set them directly are unaffected. Returns an error naming whichever
// required variable is missing.
func Load() (Config, error) {
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			return Config{}, fmt.Errorf("loading .env: %w", err)
		}
	}

	cfg := Config{
		DiscordToken: os.Getenv("DISCORD_TOKEN"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		GuildID:      os.Getenv("DISCORD_GUILD_ID"),
	}
	if cfg.DiscordToken == "" {
		return Config{}, fmt.Errorf("DISCORD_TOKEN environment variable is required")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL environment variable is required")
	}
	return cfg, nil
}

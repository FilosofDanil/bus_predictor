// Package config reads config.txt: key = value lines, # starts a comment.
// Keys: stop, line, richtung (alias: direction), telegram_token.
// The TELEGRAM_BOT_TOKEN env var overrides telegram_token so the token can be
// kept out of the file.
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Stop          string
	Line          string
	Richtung      string // optional; empty = report all directions
	TelegramToken string // non-empty = run as a Telegram bot instead of console
}

func Read(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	for raw := range strings.SplitSeq(string(data), "\n") {
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, value, found := strings.Cut(text, "=")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "stop":
			cfg.Stop = value
		case "line":
			cfg.Line = value
		case "richtung", "direction":
			cfg.Richtung = value
		case "telegram_token":
			cfg.TelegramToken = value
		default:
			return nil, fmt.Errorf("config %s: unknown key %q", path, strings.TrimSpace(key))
		}
	}
	if v := os.Getenv("TELEGRAM_BOT_TOKEN"); v != "" {
		cfg.TelegramToken = v
	}
	return cfg, nil
}

package config

import (
	"os"
	"testing"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := dir + "/" + name
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	cfg, err := Read(write("full.txt",
		"stop = X\nline = M37\nrichtung = Hahneberg\ntelegram_token = 12:ab\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Stop != "X" || cfg.Line != "M37" || cfg.Richtung != "Hahneberg" || cfg.TelegramToken != "12:ab" {
		t.Errorf("bad config: %+v", cfg)
	}

	cfg, err = Read(write("minimal.txt", "stop = X\nline = M37\n# richtung = commented\n"))
	if err != nil || cfg.Richtung != "" || cfg.TelegramToken != "" {
		t.Errorf("optional keys should default to empty: %+v, err %v", cfg, err)
	}

	if _, err := Read(write("typo.txt", "stop = X\nlien = M37\n")); err == nil {
		t.Error("expected error for unknown config key")
	}
}

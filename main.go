// Bus Predictor: shows the real position of a BVG bus ("last departed stop")
// instead of trusting the schedule. See CHAT.md for background.
//
// Runs as a Telegram bot when a telegram_token is configured, otherwise prints
// a one-shot console report for the configured stop/line.
package main

import (
	"fmt"
	"os"
	"time"

	"bus_predictor/internal/bot"
	"bus_predictor/internal/config"
	"bus_predictor/internal/engine"
)

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error :", err)
	os.Exit(1)
}

func main() {
	configPath := "config.txt"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.Read(configPath)
	if err != nil {
		fail(err)
	}

	if cfg.TelegramToken != "" {
		if err := bot.Run(cfg.TelegramToken); err != nil {
			fail(err)
		}
		return
	}

	if cfg.Stop == "" || cfg.Line == "" {
		fail(fmt.Errorf("config %s must contain 'stop = ...' and 'line = ...' (or 'telegram_token = ...' for bot mode)", configPath))
	}

	stop, err := engine.FindStop(cfg.Stop)
	if err != nil {
		fail(err)
	}
	now := time.Now()
	rep, err := engine.BuildReport(stop, cfg.Line, cfg.Richtung, now)
	if err != nil {
		fail(err)
	}
	printReport(rep, now)
}

func printReport(rep *engine.Report, now time.Time) {
	fmt.Printf("stop : %s\n", rep.StopName)
	fmt.Printf("line : %s\n", rep.Line)

	if len(rep.Directions) == 0 {
		fmt.Printf("next departure : none found among the next 100 departures at this stop\n")
		fmt.Printf("delay : n/a\n")
		fmt.Printf("last departed stop : n/a\n")
		return
	}

	for _, d := range rep.Directions {
		fmt.Println()
		fmt.Printf("richtung : %s\n", d.Richtung)
		fmt.Printf("next departure : %s (in %d min)\n", engine.Clock(d.Departure), engine.MinutesBetween(now, d.Departure))
		fmt.Printf("delay : %s\n", d.DelayText)
		switch {
		case d.LastStopErr != "":
			fmt.Printf("last departed stop : unavailable (%s)\n", d.LastStopErr)
		case d.RealTime:
			fmt.Printf("last departed stop : %s (departed %s, %d min ago)\n",
				d.LastStopName, engine.Clock(d.LastStopTime), engine.MinutesBetween(d.LastStopTime, now))
		default:
			fmt.Printf("last departed stop : %s (departed %s per schedule — no real-time confirmation)\n",
				d.LastStopName, engine.Clock(d.LastStopTime))
		}
	}
}

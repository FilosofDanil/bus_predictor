# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

A Go CLI that shows the real position of a BVG (Berlin) bus — the last stop it departed from — instead of trusting the schedule. Design rationale and API research live in `CHAT.md` (in Russian): read it before making architectural changes.

Data source is **Transitous** (`api.transitous.org`), a community-run MOTIS instance serving the official VBB GTFS + GTFS-RT feeds as free no-auth JSON. The project originally used `v6.bvg.transport.rest` (as CHAT.md suggests) but switched after that single-server service went down entirely (Aug 2026); don't switch back without checking it's stable again.

## Commands

- Build: `go build -o bus_predictor.exe .`
- Run: `.\bus_predictor.exe [config-path]` (defaults to `config.txt`) — starts the Telegram bot when `telegram_token` is configured (or `TELEGRAM_BOT_TOKEN` env var is set), otherwise prints a one-shot console report for the configured stop/line
- Test: `go test ./...` — offline; mocks both the Transitous API and the Telegram API with `httptest`
- Single test: `go test ./internal/bot -run TestBotFlow`
- Makefile: `build`, `build-linux` (static binary in `dist/`), `test`, `vet`, `run`, `clean`, `docker-build`, `docker-run` — needs GNU make (not installed natively here; use `winget install ezwinports.make`, WSL, or run make inside `golang:1.24-alpine`)
- Docker: `docker build -t bus-predictor .` then `docker run --rm bus-predictor` (console report) or `docker run --rm -e TELEGRAM_BOT_TOKEN=<token> bus-predictor` (bot). `.dockerignore` excludes `config.txt` so the real token never enters the image — the image ships `config.example.txt` as its default config; mount over `/app/config.txt` to customize

## Architecture

Root `main.go` (package main: mode selection bot-vs-console, console renderer) plus three packages under `internal/`:

- `internal/config` — `config.Read` parses config.txt (`stop`, `line`, optional `richtung`/`direction`, `telegram_token`); unknown keys are errors
- `internal/engine` — everything Transitous/MOTIS: API types, HTTP client, the pipeline, and `engine.BuildReport`, which returns a structured `engine.Report` (per-direction next departure, delay text, last departed stop) that both frontends render. `internal/engine/enginetest` holds the shared mock API server (it does not import engine, so both engine and bot tests can use it)
- `internal/bot` — `bot.Run`: Telegram Bot API over long polling (`getUpdates`), no external dependencies; a per-chat state machine: `/start` → enter stop name → choose bus number (reply keyboard of `DistinctLines`) → choose richtung or "Start (all directions)" (keyboard of `DirectionsOf` headsigns) → HTML report with the last-departed stop and arrival time in bold. `/track` follows the chat's last completed request in a goroutine: re-fetches every `trackInterval` (30 s) and sends short status lines (position + ETA only) when `dirKey` changes (position, arrival minute, delay — deliberately not the countdown). Arrival detection: each direction records its `TripID`; when the stop's next departure for that headsign becomes a different trip, the tracked bus has passed the station → arrival notice, and when no tracked directions remain, tracking ends. Auto-stops after `trackDuration` (30 min); `/untrack` cancels. The `trackers` map is mutex-guarded because tracker goroutines remove themselves on timeout/arrival

Engine pipeline:

1. `FindStop` — `/api/v1/geocode` resolves the stop name; stops with the `de-VBB_` ID prefix are preferred because Transitous is worldwide and German stop names collide
2. `ValidateLine` — checks the line appears among the stop's upcoming departures (geocode results carry no line list)
3. `FetchStopTimes` — `/api/v5/stoptimes?stopId=...&n=100`; `nextPerDirection` picks the earliest non-cancelled future departure per direction (`headsign`), filtered by `richtung` (case-insensitive substring) when set — otherwise all directions are reported
4. `lastDepartedStop` — `/api/v5/trip?tripId=...`; walks `from` + `intermediateStops` + `to` of the transit leg; last stop with a departure in the past

Report output is `<param> : <value>` lines: stop and line once, then per direction a block of richtung, next departure, delay, last departed stop.

## Critical domain rules

- MOTIS marks real-time backing explicitly: `realTime: false` means the times are pure schedule — always report those as schedule-only, never as measurements (CHAT.md documents how HAFAS masks this; MOTIS is honest about it). Delay = `departure` − `scheduledDeparture`, only meaningful when `realTime` is true.
- Discrete stop events are reliable; continuous positions are interpolated — prefer "last departed stop" over map dots.
- All MOTIS timestamps are UTC; display in Europe/Berlin. `time/tzdata` is imported blank because Windows has no system tz database.
- Grouping by `headsign` can yield more than two "directions" (short-run variants like "Falkenhagener Feld, Im Spektefeld" on M37 appear as their own direction).

## Testing note

`engine.BaseURL` is overridable via the `TRANSITOUS_API_BASE` env var and `tgBaseURL` (bot, unexported) is a plain var; tests point both at `httptest.Server`s — the shared MOTIS fixtures live in `internal/engine/enginetest`, the recording fake Telegram in `internal/bot/bot_test.go`. `TestBotFlow` drives the whole `/start → stop → line → start` conversation offline. The mock serves RFC3339 times (whole seconds), so tests that time-shift `now` for sub-minute precision (e.g. `TestTrackArrival`) must put it on a second boundary — a sub-second remainder silently truncates fixture times into the past.

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).

## Delegation

- `go-build-test` skill: run vet/build/test after any change to `internal/*` or `main.go`.
- `docker-workflow` skill: docker build/run checks, including `.dockerignore`/`config.example.txt` behavior.
- `code-scout`: "where is X handled" (e.g. tracking, keyboard flow, MOTIS pipeline) — query the graph first.
- `sonnet-worker`: mechanical edits (config fields, keyboard text, Makefile targets), formatting the `<param> : <value>` report.
- Main model: changes to the engine pipeline logic, delay/real-time semantics, or tracker concurrency (mutex-guarded `trackers` map) — needs judgement, not delegation.

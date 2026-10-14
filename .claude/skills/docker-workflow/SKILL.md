---
name: docker-workflow
description: Build and run the Bus_Predictor Docker image (console report or Telegram bot mode). Use when asked to containerize, build the docker image, run it in Docker, or test the .dockerignore / config.example.txt setup. Trigger words: docker build, docker run, containerize, docker-build, docker-run, make docker-build.
context: fork
model: sonnet
---

# Docker build + run

`.dockerignore` excludes `config.txt` so a real token never enters the image; the image ships
`config.example.txt` as its default config.

## Commands

- Build: `docker build -t bus-predictor . 2>&1 | tail -40`
- Run (console report, uses config.example.txt baked into the image): `docker run --rm bus-predictor`
- Run (Telegram bot): `docker run --rm -e TELEGRAM_BOT_TOKEN=<token> bus-predictor`
- Mount a custom config instead of the baked-in example: add `-v <host-config-path>:/app/config.txt`
- Makefile shortcuts (needs GNU make): `make docker-build`, `make docker-run`

## What to return

Only the outcome: build success/failure with the failing layer/step if it broke, and for a run, whether
the console report printed or the bot started polling — never the full build log or streamed bot output.
Never place a real Telegram token in a command that gets logged or returned; treat it as a placeholder.

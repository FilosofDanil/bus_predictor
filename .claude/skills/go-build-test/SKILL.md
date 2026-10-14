---
name: go-build-test
description: Build, vet, and test the Bus_Predictor Go project. Use when asked to build the binary, run the test suite, check for vet issues, or verify a code change compiles and passes tests. Trigger words: build, go build, go test, go vet, run tests, does it compile, check tests pass.
context: fork
model: sonnet
---

# Go build + test

Runs offline (mocked Transitous + Telegram APIs via httptest) — no network or real tokens needed.

## Commands

- Vet: `go vet ./... 2>&1 | tail -30`
- Build: `go build -o bus_predictor.exe . 2>&1 | tail -30`
- Full test suite: `go test ./... 2>&1 | tail -50`
- Single package/test: `go test ./internal/bot -run TestBotFlow -v 2>&1 | tail -50`

Run vet, then build, then test, in that order — stop and report if an earlier step fails.

## What to return

Only failures: the failing command, the failing package/test name(s), and the relevant error/diff lines
(compiler error, `--- FAIL:` blocks, or vet warnings). On success, return a one-line confirmation
("vet clean, build ok, N/N tests pass") — never paste full passing test output or the whole binary build log.

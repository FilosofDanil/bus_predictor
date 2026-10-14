# Bus Predictor — build / test / docker automation.
# Needs GNU make: present on Linux/macOS/WSL and in the golang Docker image;
# on native Windows install it once with `winget install ezwinports.make`
# and run from Git Bash.

BINARY ?= bus_predictor
IMAGE  ?= bus-predictor
CONFIG ?= config.txt

ifeq ($(OS),Windows_NT)
EXT := .exe
else
EXT :=
endif

.PHONY: all build build-linux test vet run clean docker-build docker-run

all: vet test build

## build: native binary for this machine (bus_predictor.exe on Windows)
build:
	go build -trimpath -o $(BINARY)$(EXT) .

## build-linux: static Linux binary in dist/ (same build the Docker image uses)
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/$(BINARY)-linux-amd64 .

## test: offline test suite (APIs are mocked)
test:
	go test ./... -count=1

vet:
	go vet ./...

## run: build then run with $(CONFIG) — bot mode if a telegram token is set
run: build
	./$(BINARY)$(EXT) $(CONFIG)

clean:
	rm -f $(BINARY)$(EXT)
	rm -rf dist

## docker-build: build the $(IMAGE) image (config.txt is dockerignored — the
## image carries a token-free default config)
docker-build:
	docker build -t $(IMAGE) .

## docker-run: one-shot console report; export TELEGRAM_BOT_TOKEN first to
## start the Telegram bot instead
docker-run:
	docker run --rm -e TELEGRAM_BOT_TOKEN $(IMAGE)

# syntax=docker/dockerfile:1
# Multi-stage build: compile a static binary, ship it on a minimal Alpine base.
# The real config.txt (with the Telegram token) is excluded via .dockerignore;
# the image carries config.example.txt as its default config instead.
#
#   docker build -t bus-predictor .
#   docker run --rm bus-predictor                                  # console report
#   docker run --rm -e TELEGRAM_BOT_TOKEN=<token> bus-predictor    # Telegram bot
#   docker run --rm -v ./my.txt:/app/config.txt bus-predictor      # custom config

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
# CGO off for a static binary; tzdata is embedded via time/tzdata
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bus_predictor .

FROM alpine:3.22
# both Transitous and the Telegram API are HTTPS
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/bus_predictor .
COPY config.example.txt config.txt
ENTRYPOINT ["/app/bus_predictor"]
CMD ["config.txt"]

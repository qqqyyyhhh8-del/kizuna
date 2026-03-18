# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /out/kizuna ./cmd/kizuna

FROM golang:1.25-bookworm

WORKDIR /app

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates git \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=build /out/kizuna /usr/local/bin/kizuna

ENV BOT_CONFIG_FILE=/app/config/bot_config.json
ENV BOT_SQLITE_PATH=/app/var/bot.db
ENV PLUGINS_DIR=/app/var/plugins

VOLUME ["/app/config", "/app/var"]

CMD ["kizuna"]

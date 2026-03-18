# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /out/discordbot ./cmd/discordbot

FROM golang:1.25-bookworm

WORKDIR /app

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates git \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=build /out/discordbot /usr/local/bin/discordbot

ENV BOT_SQLITE_PATH=/data/bot.db
ENV BOT_CONFIG_FILE=/data/bot_config.json
ENV PLUGINS_DIR=/data/plugins

VOLUME ["/data"]

CMD ["discordbot"]

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"discordbot/internal/bot"
	"discordbot/internal/config"
	"discordbot/internal/memory"
	"discordbot/internal/openai"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	openAI := openai.NewClient(cfg.OpenAI)
	store := memory.NewStore(openAI.Embed)

	handler := bot.NewHandler(cfg.Bot, openAI.Chat, openAI.Embed, store)

	session, err := bot.NewSession(cfg.Bot.DiscordToken, handler)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	if err := session.Open(); err != nil {
		log.Fatal(err)
	}
	log.Println("Bot is running. Press CTRL-C to exit.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = session.CloseWithContext(ctx)
}

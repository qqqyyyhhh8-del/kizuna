package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"kizuna/internal/bot"
	"kizuna/internal/buildinfo"
	"kizuna/internal/config"
	"kizuna/internal/memory"
	"kizuna/internal/openai"
	"kizuna/internal/pluginhost"
	"kizuna/internal/runtimecfg"
	sqlitestorage "kizuna/internal/storage/sqlite"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	openAI := openai.NewClient(cfg.OpenAI)
	db, err := sqlitestorage.Open(cfg.Bot.SQLitePath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	store, err := memory.NewStoreWithDB(openAI.Embed, db)
	if err != nil {
		log.Fatal(err)
	}
	runtimeStore, err := runtimecfg.OpenWithDB(db, cfg.Bot.ConfigFilePath)
	if err != nil {
		log.Fatal(err)
	}
	var rerankFn bot.RerankFn
	if openAI.CanRerank() {
		rerankFn = openAI.Rerank
	}

	handler := bot.NewHandler(cfg.Bot, openAI.Chat, openAI.Embed, rerankFn, store, runtimeStore)
	pluginManager, err := pluginhost.NewManager(pluginhost.Config{
		PluginsDir:                cfg.Bot.PluginsDir,
		DB:                        db,
		HostVersion:               buildinfo.Version,
		RuntimeStore:              runtimeStore,
		ChatFn:                    openAI.Chat,
		EmbedFn:                   openAI.Embed,
		RerankFn:                  pluginhost.RerankFn(rerankFn),
		ReservedCommands:          bot.CoreSlashCommandNames(),
		ReservedComponentPrefixes: bot.CoreComponentPrefixes(),
	})
	if err != nil {
		log.Fatal(err)
	}
	handler.SetPluginManager(pluginManager)

	session, err := bot.NewSession(cfg.Bot.DiscordToken, cfg.Bot.CommandGuildID, handler)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	if err := session.Open(); err != nil {
		log.Fatal(err)
	}
	log.Printf("Kizuna is running (%s). Press CTRL-C to exit.", buildinfo.Version)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = session.CloseWithContext(ctx)
}

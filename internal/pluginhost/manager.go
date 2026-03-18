package pluginhost

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"discordbot/internal/openai"
	"discordbot/internal/runtimecfg"
	"discordbot/pkg/pluginapi"

	"github.com/bwmarrin/discordgo"
)

const (
	pluginManifestFile      = "plugin.json"
	pluginCallTimeout       = 5 * time.Second
	pluginInitializeTimeout = 10 * time.Second
)

type ChatFn func(ctx context.Context, messages []openai.ChatMessage) (string, error)
type EmbedFn func(ctx context.Context, input string) ([]float64, error)
type RerankFn func(ctx context.Context, query string, documents []string, topN int) ([]string, error)
type SendMessageFn func(ctx context.Context, request pluginapi.SendMessageRequest) error
type ReplyToMessageFn func(ctx context.Context, message pluginapi.MessageContext) error
type ListGuildEmojisFn func(ctx context.Context, guildID string) ([]pluginapi.GuildEmoji, error)
type RefreshCommandsFn func() error

type Config struct {
	PluginsDir                string
	DB                        *sql.DB
	HostVersion               string
	RuntimeStore              *runtimecfg.Store
	ChatFn                    ChatFn
	EmbedFn                   EmbedFn
	RerankFn                  RerankFn
	ReservedCommands          []string
	ReservedComponentPrefixes []string
}

type Manager struct {
	registry     *Registry
	runtimeStore *runtimecfg.Store
	hostVersion  string
	chatFn       ChatFn
	embedFn      EmbedFn
	rerankFn     RerankFn

	mu        sync.RWMutex
	processes map[string]*managedPlugin
	closing   bool

	sendMessageFn     SendMessageFn
	replyToMessageFn  ReplyToMessageFn
	listGuildEmojisFn ListGuildEmojisFn
	refreshCommandsFn RefreshCommandsFn
	botUserID         string

	reservedCommands          map[string]struct{}
	reservedComponentPrefixes []string
}

type managedPlugin struct {
	install InstalledPlugin
	cmd     *exec.Cmd
	session *pluginapi.RPCSession

	mu           sync.Mutex
	restartTimes []time.Time
}

func NewManager(cfg Config) (*Manager, error) {
	var (
		registry *Registry
		err      error
	)
	if cfg.DB != nil {
		registry, err = OpenRegistryWithDB(cfg.DB, cfg.PluginsDir)
	} else {
		registry, err = OpenRegistry(cfg.PluginsDir)
	}
	if err != nil {
		return nil, err
	}

	manager := &Manager{
		registry:                  registry,
		runtimeStore:              cfg.RuntimeStore,
		hostVersion:               strings.TrimSpace(cfg.HostVersion),
		chatFn:                    cfg.ChatFn,
		embedFn:                   cfg.EmbedFn,
		rerankFn:                  cfg.RerankFn,
		processes:                 map[string]*managedPlugin{},
		reservedCommands:          make(map[string]struct{}, len(cfg.ReservedCommands)),
		reservedComponentPrefixes: normalizeStrings(cfg.ReservedComponentPrefixes),
	}
	for _, command := range normalizeStrings(cfg.ReservedCommands) {
		manager.reservedCommands[command] = struct{}{}
	}
	return manager, nil
}

func (m *Manager) SetBotUserID(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.botUserID = strings.TrimSpace(userID)
}

func (m *Manager) SetSendMessageFn(fn SendMessageFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendMessageFn = fn
}

func (m *Manager) SetReplyToMessageFn(fn ReplyToMessageFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replyToMessageFn = fn
}

func (m *Manager) SetListGuildEmojisFn(fn ListGuildEmojisFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listGuildEmojisFn = fn
}

func (m *Manager) SetRefreshCommandsFn(fn RefreshCommandsFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshCommandsFn = fn
}

func (m *Manager) Start() error {
	for _, plugin := range m.registry.List() {
		if !plugin.Enabled {
			continue
		}
		if _, err := m.ensurePluginRunning(plugin.ID); err != nil {
			log.Printf("plugin start failed: plugin=%s err=%v", plugin.ID, err)
		}
	}
	return nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	m.closing = true
	processes := make([]*managedPlugin, 0, len(m.processes))
	for _, process := range m.processes {
		processes = append(processes, process)
	}
	m.mu.Unlock()

	for _, process := range processes {
		_ = m.stopPlugin(process.install.ID)
	}
	return nil
}

func (m *Manager) List() []InstalledPlugin {
	return m.registry.List()
}

func (m *Manager) ApplicationCommands() ([]*discordgo.ApplicationCommand, error) {
	plugins := m.registry.List()
	commands := make([]*discordgo.ApplicationCommand, 0)
	seen := map[string]string{}
	for _, plugin := range plugins {
		if !plugin.Enabled {
			continue
		}
		for _, command := range plugin.Manifest.Commands {
			if owner, ok := seen[command.Name]; ok {
				return nil, fmt.Errorf("plugin command conflict: %s also declared by %s", command.Name, owner)
			}
			seen[command.Name] = plugin.ID
			commands = append(commands, convertCommandSpec(command))
		}
	}
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})
	return commands, nil
}

func (m *Manager) CanHandleSlashCommand(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, plugin := range m.registry.List() {
		if !plugin.Enabled {
			continue
		}
		for _, command := range plugin.Manifest.Commands {
			if command.Name == name {
				return true
			}
		}
	}
	return false
}

func (m *Manager) CanHandleComponent(customID string) bool {
	plugin, ok := m.findPluginByComponentID(customID)
	return ok && plugin.Enabled
}

func (m *Manager) CanHandleModal(customID string) bool {
	plugin, ok := m.findPluginByComponentID(customID)
	return ok && plugin.Enabled
}

func (m *Manager) HandleSlashCommand(ctx context.Context, request pluginapi.SlashCommandRequest) (*pluginapi.InteractionResponse, error) {
	plugin, command, ok := m.findPluginByCommand(request.CommandName)
	if !ok {
		return nil, errors.New("plugin command not found")
	}
	if err := m.requireCapability(plugin, pluginapi.CapabilityDiscordInteractionRespond); err != nil {
		return nil, err
	}
	if !m.registry.AllowsGuild(plugin, request.Guild.ID) {
		return denyInteractionResponse("插件未在当前服务器启用。"), nil
	}
	process, err := m.ensurePluginRunning(plugin.ID)
	if err != nil {
		return nil, err
	}

	request.PluginID = plugin.ID
	request.User = m.enrichUserInfo(request.User)
	callCtx, cancel := context.WithTimeout(ctx, pluginCallTimeout)
	defer cancel()

	var response *pluginapi.InteractionResponse
	if err := process.session.Call(callCtx, pluginapi.MethodPluginOnSlashCommand, request, &response); isMethodNotFound(err) {
		return denyInteractionResponse("插件未实现 Slash 命令处理。"), nil
	} else if err != nil {
		return nil, err
	}
	if response == nil {
		return &pluginapi.InteractionResponse{
			Type: pluginapi.InteractionResponseTypeMessage,
			Message: &pluginapi.InteractionMessage{
				Content:   fmt.Sprintf("插件命令 `%s` 已执行。", command.Name),
				Ephemeral: true,
			},
		}, nil
	}
	return response, nil
}

func (m *Manager) HandleComponent(ctx context.Context, request pluginapi.ComponentRequest) (*pluginapi.InteractionResponse, error) {
	plugin, ok := m.findPluginByComponentID(request.CustomID)
	if !ok {
		return nil, errors.New("plugin component not found")
	}
	if err := m.requireCapability(plugin, pluginapi.CapabilityDiscordInteractionRespond); err != nil {
		return nil, err
	}
	if !m.registry.AllowsGuild(plugin, request.Guild.ID) {
		return denyInteractionResponse("插件未在当前服务器启用。"), nil
	}
	process, err := m.ensurePluginRunning(plugin.ID)
	if err != nil {
		return nil, err
	}

	request.PluginID = plugin.ID
	request.User = m.enrichUserInfo(request.User)
	callCtx, cancel := context.WithTimeout(ctx, pluginCallTimeout)
	defer cancel()

	var response *pluginapi.InteractionResponse
	if err := process.session.Call(callCtx, pluginapi.MethodPluginOnComponent, request, &response); err != nil {
		return nil, err
	}
	return response, nil
}

func (m *Manager) HandleModal(ctx context.Context, request pluginapi.ModalRequest) (*pluginapi.InteractionResponse, error) {
	plugin, ok := m.findPluginByComponentID(request.CustomID)
	if !ok {
		return nil, errors.New("plugin modal not found")
	}
	if err := m.requireCapability(plugin, pluginapi.CapabilityDiscordInteractionRespond); err != nil {
		return nil, err
	}
	if !m.registry.AllowsGuild(plugin, request.Guild.ID) {
		return denyInteractionResponse("插件未在当前服务器启用。"), nil
	}
	process, err := m.ensurePluginRunning(plugin.ID)
	if err != nil {
		return nil, err
	}

	request.PluginID = plugin.ID
	request.User = m.enrichUserInfo(request.User)
	callCtx, cancel := context.WithTimeout(ctx, pluginCallTimeout)
	defer cancel()

	var response *pluginapi.InteractionResponse
	if err := process.session.Call(callCtx, pluginapi.MethodPluginOnModal, request, &response); err != nil {
		return nil, err
	}
	return response, nil
}

func (m *Manager) DispatchMessage(ctx context.Context, event pluginapi.MessageEvent) {
	for _, plugin := range m.registry.List() {
		if !plugin.Enabled || !m.registry.AllowsGuild(plugin, event.Message.Guild.ID) {
			continue
		}
		process, err := m.ensurePluginRunning(plugin.ID)
		if err != nil {
			log.Printf("plugin message dispatch start failed: plugin=%s err=%v", plugin.ID, err)
			continue
		}
		request := event
		request.PluginID = plugin.ID
		request.Message.Author = m.enrichUserInfo(request.Message.Author)

		callCtx, cancel := context.WithTimeout(ctx, pluginCallTimeout)
		err = process.session.Call(callCtx, pluginapi.MethodPluginOnMessage, request, nil)
		cancel()
		if isMethodNotFound(err) || err == nil {
			continue
		}
		log.Printf("plugin message dispatch failed: plugin=%s err=%v", plugin.ID, err)
	}
}

func (m *Manager) BuildPromptBlocks(ctx context.Context, request pluginapi.PromptBuildRequest) ([]pluginapi.PromptBlock, error) {
	var blocks []pluginapi.PromptBlock
	for _, plugin := range m.registry.List() {
		if !plugin.Enabled || !m.registry.AllowsGuild(plugin, request.CurrentMessage.Guild.ID) {
			continue
		}
		process, err := m.ensurePluginRunning(plugin.ID)
		if err != nil {
			log.Printf("plugin prompt build start failed: plugin=%s err=%v", plugin.ID, err)
			continue
		}

		callRequest := request
		callRequest.PluginID = plugin.ID
		callCtx, cancel := context.WithTimeout(ctx, pluginCallTimeout)
		var response *pluginapi.PromptBuildResponse
		err = process.session.Call(callCtx, pluginapi.MethodPluginOnPromptBuild, callRequest, &response)
		cancel()
		if isMethodNotFound(err) || response == nil {
			continue
		}
		if err != nil {
			log.Printf("plugin prompt build failed: plugin=%s err=%v", plugin.ID, err)
			continue
		}
		blocks = append(blocks, response.Blocks...)
	}
	return blocks, nil
}

func (m *Manager) PostprocessResponse(ctx context.Context, request pluginapi.ResponsePostprocessRequest) (string, error) {
	response := request.Response
	for _, plugin := range m.registry.List() {
		if !plugin.Enabled || !m.registry.AllowsGuild(plugin, request.CurrentMessage.Guild.ID) {
			continue
		}
		process, err := m.ensurePluginRunning(plugin.ID)
		if err != nil {
			log.Printf("plugin response postprocess start failed: plugin=%s err=%v", plugin.ID, err)
			continue
		}

		callRequest := request
		callRequest.PluginID = plugin.ID
		callRequest.Response = response
		callCtx, cancel := context.WithTimeout(ctx, pluginCallTimeout)
		var postprocessed *pluginapi.ResponsePostprocessResponse
		err = process.session.Call(callCtx, pluginapi.MethodPluginOnResponsePostprocess, callRequest, &postprocessed)
		cancel()
		if isMethodNotFound(err) || postprocessed == nil {
			continue
		}
		if err != nil {
			log.Printf("plugin response postprocess failed: plugin=%s err=%v", plugin.ID, err)
			continue
		}
		if strings.TrimSpace(postprocessed.Response) != "" {
			response = strings.TrimSpace(postprocessed.Response)
		}
	}
	return response, nil
}

func (m *Manager) ensurePluginRunning(pluginID string) (*managedPlugin, error) {
	m.mu.RLock()
	process := m.processes[strings.TrimSpace(pluginID)]
	m.mu.RUnlock()
	if process != nil {
		return process, nil
	}

	plugin, ok := m.registry.Get(pluginID)
	if !ok {
		return nil, errors.New("plugin not found")
	}
	if !plugin.Enabled {
		return nil, errors.New("plugin is disabled")
	}

	return m.startPlugin(plugin)
}

func (m *Manager) startPlugin(plugin InstalledPlugin) (*managedPlugin, error) {
	m.mu.Lock()
	if existing := m.processes[plugin.ID]; existing != nil {
		m.mu.Unlock()
		return existing, nil
	}
	m.mu.Unlock()

	rootDir := pluginRootDir(plugin)
	commandPath := strings.TrimSpace(plugin.Manifest.Runtime.Command)
	if commandPath == "" {
		return nil, errors.New("plugin runtime command is required")
	}
	if strings.HasPrefix(commandPath, ".") || strings.Contains(commandPath, string(os.PathSeparator)) {
		commandPath = filepath.Join(rootDir, commandPath)
	}

	cmd := exec.Command(commandPath, plugin.Manifest.Runtime.Args...)
	cmd.Dir = rootDir
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	process := &managedPlugin{
		install: plugin,
		cmd:     cmd,
		session: pluginapi.NewRPCSession(stdout, stdin),
	}
	m.registerHostHandlers(process)

	initCtx, cancel := context.WithTimeout(context.Background(), pluginInitializeTimeout)
	defer cancel()
	var initResponse struct{}
	if err := process.session.Call(initCtx, pluginapi.MethodPluginInitialize, pluginapi.InitializeRequest{
		Host: pluginapi.HostInfo{
			Version:   m.hostVersion,
			BotUserID: m.botUserID,
		},
		Manifest: plugin.Manifest,
	}, &initResponse); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = cmd.Process.Kill()
		return nil, err
	}

	m.mu.Lock()
	if existing := m.processes[plugin.ID]; existing == nil {
		m.processes[plugin.ID] = process
	}
	m.mu.Unlock()
	_ = m.registry.SetLastError(plugin.ID, "")

	go m.waitPlugin(process, stdout, stdin)
	if plugin.Manifest.IntervalSeconds > 0 {
		go m.runIntervals(process)
	}
	return process, nil
}

func (m *Manager) stopPlugin(pluginID string) error {
	m.mu.Lock()
	process := m.processes[strings.TrimSpace(pluginID)]
	delete(m.processes, strings.TrimSpace(pluginID))
	m.mu.Unlock()
	if process == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = process.session.Call(ctx, pluginapi.MethodPluginShutdown, pluginapi.ShutdownRequest{}, nil)
	process.session.CloseWithError(io.EOF)
	if process.cmd.Process != nil {
		_ = process.cmd.Process.Kill()
	}
	return nil
}

func (m *Manager) waitPlugin(process *managedPlugin, stdout io.ReadCloser, stdin io.WriteCloser) {
	err := process.cmd.Wait()
	_ = stdout.Close()
	_ = stdin.Close()
	process.session.CloseWithError(err)
	m.handlePluginExit(process, err)
}

func (m *Manager) handlePluginExit(process *managedPlugin, err error) {
	m.mu.Lock()
	if current := m.processes[process.install.ID]; current == process {
		delete(m.processes, process.install.ID)
	}
	closing := m.closing
	m.mu.Unlock()
	if closing {
		return
	}

	now := time.Now()
	process.mu.Lock()
	filtered := process.restartTimes[:0]
	for _, item := range process.restartTimes {
		if now.Sub(item) <= 5*time.Minute {
			filtered = append(filtered, item)
		}
	}
	filtered = append(filtered, now)
	process.restartTimes = filtered
	attempts := len(filtered)
	process.mu.Unlock()

	if attempts >= 3 {
		message := fmt.Sprintf("插件进程异常退出，5 分钟内失败 %d 次: %v", attempts, err)
		_ = m.registry.SetLastError(process.install.ID, message)
		log.Printf("plugin marked unhealthy: plugin=%s err=%v", process.install.ID, err)
		return
	}

	delay := 1 * time.Second
	if attempts == 2 {
		delay = 5 * time.Second
	}
	if attempts >= 3 {
		delay = 30 * time.Second
	}
	go func(pluginID string, delay time.Duration) {
		time.Sleep(delay)
		plugin, ok := m.registry.Get(pluginID)
		if !ok || !plugin.Enabled {
			return
		}
		if _, startErr := m.startPlugin(plugin); startErr != nil {
			log.Printf("plugin restart failed: plugin=%s err=%v", pluginID, startErr)
			_ = m.registry.SetLastError(pluginID, startErr.Error())
		}
	}(process.install.ID, delay)
}

func (m *Manager) runIntervals(process *managedPlugin) {
	interval := time.Duration(process.install.Manifest.IntervalSeconds) * time.Second
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		m.mu.RLock()
		current := m.processes[process.install.ID]
		closing := m.closing
		m.mu.RUnlock()
		if closing || current != process {
			return
		}

		<-ticker.C
		callCtx, cancel := context.WithTimeout(context.Background(), pluginCallTimeout)
		err := process.session.Call(callCtx, pluginapi.MethodPluginOnInterval, pluginapi.IntervalRequest{
			PluginID: process.install.ID,
			FiredAt:  time.Now().Format(time.RFC3339),
		}, nil)
		cancel()
		if isMethodNotFound(err) || err == nil {
			continue
		}
		log.Printf("plugin interval failed: plugin=%s err=%v", process.install.ID, err)
	}
}

func (m *Manager) registerHostHandlers(process *managedPlugin) {
	process.session.RegisterHandler(pluginapi.MethodHostStorageGet, func(ctx context.Context, params json.RawMessage) (any, error) {
		if err := m.requireCapability(process.install, pluginapi.CapabilityPluginStorage); err != nil {
			return nil, err
		}
		var request pluginapi.StorageGetRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		value, ok := m.registry.StorageGet(process.install.ID, request.Key)
		return pluginapi.StorageGetResponse{
			Found: ok,
			Value: value,
		}, nil
	})
	process.session.RegisterHandler(pluginapi.MethodHostStorageSet, func(ctx context.Context, params json.RawMessage) (any, error) {
		if err := m.requireCapability(process.install, pluginapi.CapabilityPluginStorage); err != nil {
			return nil, err
		}
		var request pluginapi.StorageSetRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return struct{}{}, m.registry.StorageSet(process.install.ID, request.Key, request.Value)
	})
	process.session.RegisterHandler(pluginapi.MethodHostListGuildEmojis, func(ctx context.Context, params json.RawMessage) (any, error) {
		if err := m.requireCapability(process.install, pluginapi.CapabilityDiscordReadGuildEmojis); err != nil {
			return nil, err
		}
		var request pluginapi.ListGuildEmojisRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		m.mu.RLock()
		listGuildEmojisFn := m.listGuildEmojisFn
		m.mu.RUnlock()
		if listGuildEmojisFn == nil {
			return nil, errors.New("guild emoji lister is unavailable")
		}
		emojis, err := listGuildEmojisFn(ctx, request.GuildID)
		if err != nil {
			return nil, err
		}
		return pluginapi.ListGuildEmojisResponse{Emojis: emojis}, nil
	})
	process.session.RegisterHandler(pluginapi.MethodHostChat, func(ctx context.Context, params json.RawMessage) (any, error) {
		if err := m.requireCapability(process.install, pluginapi.CapabilityLLMChat); err != nil {
			return nil, err
		}
		if m.chatFn == nil {
			return nil, errors.New("chat host is unavailable")
		}
		var request pluginapi.ChatRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		content, err := m.chatFn(ctx, convertPluginChatMessages(request.Messages))
		if err != nil {
			return nil, err
		}
		return pluginapi.ChatResponse{Content: content}, nil
	})
	process.session.RegisterHandler(pluginapi.MethodHostEmbed, func(ctx context.Context, params json.RawMessage) (any, error) {
		if err := m.requireCapability(process.install, pluginapi.CapabilityLLMEmbed); err != nil {
			return nil, err
		}
		if m.embedFn == nil {
			return nil, errors.New("embed host is unavailable")
		}
		var request pluginapi.EmbedRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		vector, err := m.embedFn(ctx, request.Input)
		if err != nil {
			return nil, err
		}
		return pluginapi.EmbedResponse{Vector: vector}, nil
	})
	process.session.RegisterHandler(pluginapi.MethodHostRerank, func(ctx context.Context, params json.RawMessage) (any, error) {
		if err := m.requireCapability(process.install, pluginapi.CapabilityLLMRerank); err != nil {
			return nil, err
		}
		if m.rerankFn == nil {
			return nil, errors.New("rerank host is unavailable")
		}
		var request pluginapi.RerankRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		documents, err := m.rerankFn(ctx, request.Query, request.Documents, request.TopN)
		if err != nil {
			return nil, err
		}
		return pluginapi.RerankResponse{Documents: documents}, nil
	})
	process.session.RegisterHandler(pluginapi.MethodHostSendMessage, func(ctx context.Context, params json.RawMessage) (any, error) {
		if err := m.requireCapability(process.install, pluginapi.CapabilityDiscordSendMessage); err != nil {
			return nil, err
		}
		var request pluginapi.SendMessageRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		if !m.runtimeStore.AllowsSpeech(request.GuildID, request.ChannelID, request.ThreadID) {
			return nil, errors.New("speech is not allowed at this location")
		}
		m.mu.RLock()
		sendMessageFn := m.sendMessageFn
		m.mu.RUnlock()
		if sendMessageFn == nil {
			return nil, errors.New("discord sender is unavailable")
		}
		return struct{}{}, sendMessageFn(ctx, request)
	})
	process.session.RegisterHandler(pluginapi.MethodHostReplyToMessage, func(ctx context.Context, params json.RawMessage) (any, error) {
		if err := m.requireCapability(process.install, pluginapi.CapabilityDiscordReplyWithCore); err != nil {
			return nil, err
		}
		var request pluginapi.ReplyToMessageRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		if !m.runtimeStore.AllowsSpeech(request.Message.Guild.ID, request.Message.Channel.ID, request.Message.Channel.ThreadID) {
			return nil, errors.New("speech is not allowed at this location")
		}
		m.mu.RLock()
		replyToMessageFn := m.replyToMessageFn
		m.mu.RUnlock()
		if replyToMessageFn == nil {
			return nil, errors.New("core reply callback is unavailable")
		}
		return struct{}{}, replyToMessageFn(ctx, request.Message)
	})
	process.session.RegisterHandler(pluginapi.MethodHostSpeechAllowed, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request pluginapi.SpeechAllowedRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return pluginapi.SpeechAllowedResponse{
			Allowed: m.runtimeStore.AllowsSpeech(request.GuildID, request.ChannelID, request.ThreadID),
		}, nil
	})
	process.session.RegisterHandler(pluginapi.MethodHostGetWorldBook, func(ctx context.Context, params json.RawMessage) (any, error) {
		if err := m.requireCapability(process.install, pluginapi.CapabilityWorldBookRead); err != nil {
			return nil, err
		}
		var request pluginapi.GetWorldBookRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		key := strings.TrimSpace(request.Key)
		if key == "" {
			return pluginapi.GetWorldBookResponse{Found: false}, nil
		}
		guildID := ""
		if index := strings.LastIndex(key, ":guild:"); index >= 0 {
			guildID = strings.TrimSpace(key[index+len(":guild:"):])
		}
		entry, ok := m.runtimeStore.WorldBookEntriesForGuild(guildID)[key]
		if !ok {
			return pluginapi.GetWorldBookResponse{Found: false}, nil
		}
		return pluginapi.GetWorldBookResponse{
			Found:   true,
			Title:   strings.TrimSpace(entry.Title),
			Content: strings.TrimSpace(entry.Content),
			GuildID: strings.TrimSpace(entry.GuildID),
			Source:  strings.TrimSpace(entry.Source),
		}, nil
	})
	process.session.RegisterHandler(pluginapi.MethodHostUpsertWorldBook, func(ctx context.Context, params json.RawMessage) (any, error) {
		if err := m.requireCapability(process.install, pluginapi.CapabilityWorldBookWrite); err != nil {
			return nil, err
		}
		var request pluginapi.UpsertWorldBookRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return struct{}{}, m.runtimeStore.UpsertWorldBookEntry(request.Key, runtimecfg.WorldBookEntry{
			Title:     strings.TrimSpace(request.Title),
			Content:   strings.TrimSpace(request.Content),
			GuildID:   strings.TrimSpace(request.GuildID),
			Source:    firstNonEmpty(strings.TrimSpace(request.Source), "plugin:"+process.install.ID),
			UpdatedAt: time.Now().Format("2006-01-02 15:04:05 UTC+8"),
		})
	})
	process.session.RegisterHandler(pluginapi.MethodHostDeleteWorldBook, func(ctx context.Context, params json.RawMessage) (any, error) {
		if err := m.requireCapability(process.install, pluginapi.CapabilityWorldBookWrite); err != nil {
			return nil, err
		}
		var request pluginapi.DeleteWorldBookRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return struct{}{}, m.runtimeStore.DeleteWorldBookEntry(request.Key)
	})
	process.session.RegisterHandler(pluginapi.MethodHostLog, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request pluginapi.LogRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		level := strings.ToUpper(strings.TrimSpace(request.Level))
		if level == "" {
			level = "INFO"
		}
		log.Printf("plugin[%s] %s: %s", process.install.ID, level, strings.TrimSpace(request.Message))
		return struct{}{}, nil
	})
}

func (m *Manager) enrichUserInfo(user pluginapi.UserInfo) pluginapi.UserInfo {
	user.ID = strings.TrimSpace(user.ID)
	if user.ID == "" || m.runtimeStore == nil {
		return user
	}
	user.IsAdmin = m.runtimeStore.IsAdmin(user.ID)
	user.IsSuperAdmin = m.runtimeStore.IsSuperAdmin(user.ID)
	return user
}

func (m *Manager) requireCapability(plugin InstalledPlugin, capability pluginapi.Capability) error {
	for _, item := range plugin.GrantedCaps {
		if item == capability {
			return nil
		}
	}
	return fmt.Errorf("plugin %s lacks capability %s", plugin.ID, capability)
}

func (m *Manager) findPluginByCommand(commandName string) (InstalledPlugin, pluginapi.CommandSpec, bool) {
	commandName = strings.TrimSpace(commandName)
	for _, plugin := range m.registry.List() {
		for _, command := range plugin.Manifest.Commands {
			if command.Name == commandName {
				return plugin, command, true
			}
		}
	}
	return InstalledPlugin{}, pluginapi.CommandSpec{}, false
}

func (m *Manager) findPluginByComponentID(customID string) (InstalledPlugin, bool) {
	customID = strings.TrimSpace(customID)
	for _, plugin := range m.registry.List() {
		for _, prefix := range plugin.Manifest.ComponentPrefixes {
			if strings.HasPrefix(customID, prefix) {
				return plugin, true
			}
		}
	}
	return InstalledPlugin{}, false
}

func convertCommandSpec(command pluginapi.CommandSpec) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        command.Name,
		Description: command.Description,
		Options:     convertCommandOptions(command.Options),
	}
}

func convertCommandOptions(options []pluginapi.CommandOption) []*discordgo.ApplicationCommandOption {
	converted := make([]*discordgo.ApplicationCommandOption, 0, len(options))
	for _, option := range options {
		converted = append(converted, &discordgo.ApplicationCommandOption{
			Type:        convertCommandOptionType(option.Type),
			Name:        option.Name,
			Description: option.Description,
			Required:    option.Required,
			Options:     convertCommandOptions(option.Options),
		})
	}
	return converted
}

func convertCommandOptionType(optionType pluginapi.CommandOptionType) discordgo.ApplicationCommandOptionType {
	switch optionType {
	case pluginapi.CommandOptionTypeInteger:
		return discordgo.ApplicationCommandOptionInteger
	case pluginapi.CommandOptionTypeNumber:
		return discordgo.ApplicationCommandOptionNumber
	case pluginapi.CommandOptionTypeBoolean:
		return discordgo.ApplicationCommandOptionBoolean
	case pluginapi.CommandOptionTypeUser:
		return discordgo.ApplicationCommandOptionUser
	case pluginapi.CommandOptionTypeChannel:
		return discordgo.ApplicationCommandOptionChannel
	case pluginapi.CommandOptionTypeRole:
		return discordgo.ApplicationCommandOptionRole
	case pluginapi.CommandOptionTypeSubcommand:
		return discordgo.ApplicationCommandOptionSubCommand
	default:
		return discordgo.ApplicationCommandOptionString
	}
}

func convertPluginChatMessages(messages []pluginapi.ChatMessage) []openai.ChatMessage {
	converted := make([]openai.ChatMessage, 0, len(messages))
	for _, message := range messages {
		parts := make([]openai.ChatContentPart, 0, len(message.Parts))
		for _, part := range message.Parts {
			parts = append(parts, openai.ChatContentPart{
				Type: part.Type,
				Text: part.Text,
				ImageURL: func() *openai.ChatImageURL {
					if part.ImageURL == nil {
						return nil
					}
					return &openai.ChatImageURL{URL: part.ImageURL.URL}
				}(),
			})
		}
		converted = append(converted, openai.ChatMessage{
			Role:    message.Role,
			Content: message.Content,
			Parts:   parts,
		})
	}
	return converted
}

func pluginRootDir(plugin InstalledPlugin) string {
	root := strings.TrimSpace(plugin.RepoDir)
	if strings.TrimSpace(plugin.PluginSubdir) == "" {
		return root
	}
	return filepath.Join(root, plugin.PluginSubdir)
}

func isMethodNotFound(err error) bool {
	var rpcErr *pluginapi.RPCError
	if errors.As(err, &rpcErr) {
		return rpcErr.Code == -32601
	}
	return false
}

func denyInteractionResponse(message string) *pluginapi.InteractionResponse {
	return &pluginapi.InteractionResponse{
		Type: pluginapi.InteractionResponseTypeMessage,
		Message: &pluginapi.InteractionMessage{
			Content:   strings.TrimSpace(message),
			Ephemeral: true,
		},
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

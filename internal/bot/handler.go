package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"kizuna/internal/config"
	"kizuna/internal/memory"
	"kizuna/internal/openai"
	"kizuna/internal/pluginhost"
	"kizuna/internal/runtimecfg"
	"kizuna/pkg/pluginapi"
)

const (
	maxHistoryMessages  = 14
	summaryTriggerCount = 18
	retrievalTopK       = 4
	rerankCandidateTopK = 12
)

type ChatFn func(ctx context.Context, messages []openai.ChatMessage) (string, error)
type EmbedFn func(ctx context.Context, input string) ([]float64, error)
type RerankFn func(ctx context.Context, query string, documents []string, topN int) ([]string, error)

type Handler struct {
	cfg           config.BotConfig
	chatFn        ChatFn
	embedFn       EmbedFn
	rerankFn      RerankFn
	store         *memory.Store
	runtimeStore  *runtimecfg.Store
	httpClient    *http.Client
	pluginManager *pluginhost.Manager

	emojiMu        sync.Mutex
	emojiAnalyzing map[string]struct{}
	randFloat64    func() float64
}

func NewHandler(cfg config.BotConfig, chatFn ChatFn, embedFn EmbedFn, rerankFn RerankFn, store *memory.Store, runtimeStore *runtimecfg.Store) *Handler {
	return &Handler{
		cfg:            cfg,
		chatFn:         chatFn,
		embedFn:        embedFn,
		rerankFn:       rerankFn,
		store:          store,
		runtimeStore:   runtimeStore,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		emojiAnalyzing: map[string]struct{}{},
		randFloat64:    rand.Float64,
	}
}

func (h *Handler) SetPluginManager(manager *pluginhost.Manager) {
	if h == nil {
		return
	}
	h.pluginManager = manager
}

func (h *Handler) HandleMessage(ctx context.Context, channelID, authorID, content string) (string, error) {
	return h.HandleMessageRecord(ctx, channelID, memory.MessageRecord{
		Role:    "user",
		Content: content,
		Author: memory.MessageAuthor{
			UserID:      authorID,
			Username:    authorID,
			DisplayName: authorID,
		},
	})
}

func (h *Handler) HandleMessageRecord(ctx context.Context, channelID string, record memory.MessageRecord) (string, error) {
	record.Role = strings.TrimSpace(record.Role)
	if record.Role == "" {
		record.Role = "user"
	}
	record.Content = strings.TrimSpace(record.Content)
	if record.Content == "" && len(record.Images) == 0 {
		return "", nil
	}
	if err := h.ensureRuntimeStore(); err != nil {
		return "", err
	}

	h.store.AddRecord(ctx, channelID, record)

	summary, recent := h.store.SummaryAndRecent(channelID)
	if shouldSummarize(recent) {
		summaryContent, err := h.chatFn(ctx, summarizationPrompt(summary, recent))
		if err != nil {
			log.Printf("summary error: %v", err)
		} else {
			h.store.SetSummary(channelID, summaryContent)
			h.store.TrimHistory(channelID, maxHistoryMessages)
			summary = summaryContent
			_, recent = h.store.SummaryAndRecent(channelID)
		}
	}

	var retrieved []string
	if strings.TrimSpace(record.Content) != "" {
		queryEmbedding, err := h.embedFn(ctx, record.Content)
		if err != nil {
			log.Printf("embed error: %v", err)
		} else {
			retrieved = h.retrieveRelevantMemories(ctx, channelID, record.Content, queryEmbedding)
		}
	}

	systemPrompt, personaPrompt := h.runtimeStore.ComposePromptsForGuild(h.cfg.SystemPrompt, record.GuildID)
	if h.pluginManager != nil && h.pluginManager.CanHandleSlashCommand("persona") {
		personaPrompt = ""
	}
	pluginBlocks := h.buildPluginPromptBlocks(ctx, systemPrompt, personaPrompt, summary, recent, retrieved, record)
	response, err := h.chatFn(ctx, buildChatMessages(systemPrompt, personaPrompt, summary, recent, retrieved, pluginBlocks))
	if err != nil {
		return "", err
	}
	response = h.postprocessPluginResponse(ctx, record, response)
	h.store.AddRecord(ctx, channelID, memory.MessageRecord{
		Role:    "assistant",
		Content: response,
	})
	return response, nil
}

func (h *Handler) retrieveRelevantMemories(ctx context.Context, channelID, query string, queryEmbedding []float64) []string {
	limit := retrievalTopK
	if h.rerankFn != nil {
		limit = rerankCandidateTopK
	}

	candidates := h.store.TopKRecords(channelID, queryEmbedding, limit)
	if len(candidates) == 0 {
		return nil
	}
	if h.rerankFn == nil || len(candidates) <= 1 {
		return takeRenderedMemories(candidates, retrievalTopK)
	}

	documents := takeMemoryContents(candidates, len(candidates))
	reranked, err := h.rerankFn(ctx, query, documents, retrievalTopK)
	if err != nil {
		log.Printf("rerank error: %v", err)
		return takeRenderedMemories(candidates, retrievalTopK)
	}
	return matchRenderedMemories(candidates, reranked, retrievalTopK)
}

func (h *Handler) ShouldProactiveReply() bool {
	if h == nil || h.runtimeStore == nil {
		return false
	}

	enabled, chance := h.runtimeStore.ProactiveReplyConfig()
	if !enabled || chance <= 0 {
		return false
	}

	random := rand.Float64
	if h.randFloat64 != nil {
		random = h.randFloat64
	}
	return random()*100 < chance
}

func (h *Handler) handlePersonaCommand(content, authorID string) (string, error) {
	command := strings.TrimSpace(strings.TrimPrefix(content, "!persona"))
	if command == "" {
		return personaHelp(), nil
	}

	switch {
	case command == "list":
		names := h.runtimeStore.PersonaNames()
		if len(names) == 0 {
			return "当前没有已保存人设。", nil
		}
		active := h.runtimeStore.ActivePersonaName()
		items := make([]string, 0, len(names))
		for _, name := range names {
			label := name
			if name == active {
				label += " (当前)"
			}
			items = append(items, label)
		}
		return "可用人设:\n- " + strings.Join(items, "\n- "), nil
	case command == "current":
		active := h.runtimeStore.ActivePersonaName()
		if active == "" {
			return "当前未启用任何人设。", nil
		}
		return fmt.Sprintf("当前启用人设: %s", active), nil
	case strings.HasPrefix(command, "show "):
		if !h.runtimeStore.IsAdmin(authorID) {
			return permissionDenied(), nil
		}
		name := strings.TrimSpace(strings.TrimPrefix(command, "show "))
		prompt, ok := h.runtimeStore.PersonaPrompt(name)
		if !ok {
			return fmt.Sprintf("人设不存在: %s", name), nil
		}
		return fmt.Sprintf("人设 %s 的 Prompt:\n%s", name, prompt), nil
	case strings.HasPrefix(command, "add "):
		if !h.runtimeStore.IsAdmin(authorID) {
			return permissionDenied(), nil
		}
		name, prompt, ok := splitNameAndBody(strings.TrimSpace(strings.TrimPrefix(command, "add ")))
		if !ok {
			return personaHelp(), nil
		}
		if err := h.runtimeStore.UpsertPersona(name, prompt); err != nil {
			return "", err
		}
		return fmt.Sprintf("已保存人设: %s", name), nil
	case strings.HasPrefix(command, "use "):
		if !h.runtimeStore.IsAdmin(authorID) {
			return permissionDenied(), nil
		}
		name := strings.TrimSpace(strings.TrimPrefix(command, "use "))
		if err := h.runtimeStore.SetActivePersona(name); err != nil {
			return err.Error(), nil
		}
		return fmt.Sprintf("已切换到人设: %s", name), nil
	case strings.HasPrefix(command, "delete "):
		if !h.runtimeStore.IsAdmin(authorID) {
			return permissionDenied(), nil
		}
		name := strings.TrimSpace(strings.TrimPrefix(command, "delete "))
		if err := h.runtimeStore.DeletePersona(name); err != nil {
			return err.Error(), nil
		}
		return fmt.Sprintf("已删除人设: %s", name), nil
	case command == "clear":
		if !h.runtimeStore.IsAdmin(authorID) {
			return permissionDenied(), nil
		}
		if err := h.runtimeStore.ClearActivePersona(); err != nil {
			return "", err
		}
		return "已清空当前人设。", nil
	default:
		return personaHelp(), nil
	}
}

func (h *Handler) handleSystemCommand(content, authorID string) (string, error) {
	command := strings.TrimSpace(strings.TrimPrefix(content, "!system"))
	if command == "" {
		return systemHelp(), nil
	}
	if !h.runtimeStore.IsAdmin(authorID) {
		return permissionDenied(), nil
	}

	switch {
	case command == "show":
		prompt := h.runtimeStore.SystemPrompt()
		if prompt == "" {
			return "当前 system prompt 为空。", nil
		}
		return "当前 system prompt:\n" + prompt, nil
	case strings.HasPrefix(command, "set "):
		prompt := strings.TrimSpace(strings.TrimPrefix(command, "set "))
		if prompt == "" {
			return systemHelp(), nil
		}
		if err := h.runtimeStore.SetSystemPrompt(prompt); err != nil {
			return "", err
		}
		return "已更新 system prompt。", nil
	case command == "clear":
		if err := h.runtimeStore.SetSystemPrompt(""); err != nil {
			return "", err
		}
		return "已清空 system prompt。", nil
	default:
		return systemHelp(), nil
	}
}

func (h *Handler) handleAdminCommand(content, authorID string) (string, error) {
	command := strings.TrimSpace(strings.TrimPrefix(content, "!admin"))
	if command == "" {
		return adminHelp(), nil
	}

	switch {
	case command == "list":
		if !h.runtimeStore.IsAdmin(authorID) {
			return permissionDenied(), nil
		}
		superAdmins, admins := h.runtimeStore.AdminLists()
		lines := []string{
			"超级管理员:",
			renderIDList(superAdmins),
			"管理员:",
			renderIDList(admins),
		}
		return strings.Join(lines, "\n"), nil
	case strings.HasPrefix(command, "add "):
		if !h.runtimeStore.IsSuperAdmin(authorID) {
			return superAdminDenied(), nil
		}
		targetID, ok := extractUserID(strings.TrimSpace(strings.TrimPrefix(command, "add ")))
		if !ok {
			return adminHelp(), nil
		}
		if err := h.runtimeStore.GrantAdmin(targetID); err != nil {
			return "", err
		}
		return fmt.Sprintf("已添加管理员: %s", targetID), nil
	case strings.HasPrefix(command, "remove "):
		if !h.runtimeStore.IsSuperAdmin(authorID) {
			return superAdminDenied(), nil
		}
		targetID, ok := extractUserID(strings.TrimSpace(strings.TrimPrefix(command, "remove ")))
		if !ok {
			return adminHelp(), nil
		}
		if err := h.runtimeStore.RevokeAdmin(targetID); err != nil {
			return err.Error(), nil
		}
		return fmt.Sprintf("已移除管理员: %s", targetID), nil
	default:
		return adminHelp(), nil
	}
}

func shouldSummarize(messages []memory.MessageRecord) bool {
	return len(messages) >= summaryTriggerCount
}

func summarizationPrompt(summary string, messages []memory.MessageRecord) []openai.ChatMessage {
	var builder strings.Builder
	if summary != "" {
		builder.WriteString("当前已知摘要:\n")
		builder.WriteString(summary)
		builder.WriteString("\n\n")
	}
	builder.WriteString("需要总结的新对话:\n")
	for _, msg := range messages {
		builder.WriteString(fmt.Sprintf("[%s]\n%s\n\n", msg.Role, renderMessageForLLM(msg)))
	}
	builder.WriteString("\n请用简洁中文更新摘要，保留关键事实、偏好和待办。")
	return []openai.ChatMessage{
		{Role: "system", Content: "你是一个对话摘要助手。"},
		{Role: "user", Content: builder.String()},
	}
}

func buildChatMessages(systemPrompt, personaPrompt, summary string, recent []memory.MessageRecord, retrieved []string, blocks []pluginapi.PromptBlock) []openai.ChatMessage {
	messages := []openai.ChatMessage{{Role: "system", Content: systemPrompt}}
	if personaPrompt != "" {
		messages = append(messages, openai.ChatMessage{
			Role:    "system",
			Content: "当前人设 Prompt:\n" + personaPrompt,
		})
	}
	if summary != "" {
		messages = append(messages, openai.ChatMessage{
			Role:    "system",
			Content: "对话摘要:\n" + summary,
		})
	}
	if len(retrieved) > 0 {
		memories := make([]string, 0, len(retrieved))
		for index, item := range retrieved {
			memories = append(memories, fmt.Sprintf("记忆 %d:\n%s", index+1, item))
		}
		messages = append(messages, openai.ChatMessage{
			Role:    "system",
			Content: "相关记忆(仅供参考):\n\n" + strings.Join(memories, "\n\n"),
		})
	}
	return buildChatMessagesWithPromptBlocks(messages, recent, blocks)
}

func buildChatMessagesWithPromptBlocks(messages []openai.ChatMessage, recent []memory.MessageRecord, blocks []pluginapi.PromptBlock) []openai.ChatMessage {
	for _, block := range blocks {
		role := strings.TrimSpace(block.Role)
		if role == "" {
			role = "system"
		}
		content := strings.TrimSpace(block.Content)
		if content == "" {
			continue
		}
		messages = append(messages, openai.ChatMessage{
			Role:    role,
			Content: content,
		})
	}
	for _, msg := range recent {
		content := renderMessageForLLM(msg)
		if len(msg.Images) == 0 {
			messages = append(messages, openai.ChatMessage{
				Role:    msg.Role,
				Content: content,
			})
			continue
		}

		parts := []openai.ChatContentPart{openai.TextPart(content)}
		for _, image := range msg.Images {
			part := openai.ImageURLPart(image.URL)
			if part.ImageURL == nil {
				continue
			}
			parts = append(parts, part)
		}
		messages = append(messages, openai.ChatMessage{
			Role:  msg.Role,
			Parts: parts,
		})
	}
	return messages
}

func renderMessageForLLM(msg memory.MessageRecord) string {
	if strings.TrimSpace(msg.Role) == "assistant" {
		return strings.TrimSpace(msg.Content)
	}
	return msg.RenderForModel()
}

func (h *Handler) buildPluginPromptBlocks(ctx context.Context, systemPrompt, personaPrompt, summary string, recent []memory.MessageRecord, retrieved []string, record memory.MessageRecord) []pluginapi.PromptBlock {
	if h == nil || h.pluginManager == nil {
		return nil
	}

	history := make([]pluginapi.PromptConversationMessage, 0, len(recent))
	for _, item := range recent {
		history = append(history, pluginapi.PromptConversationMessage{
			Role:    strings.TrimSpace(item.Role),
			Content: renderMessageForLLM(item),
		})
	}

	blocks, err := h.pluginManager.BuildPromptBlocks(ctx, pluginapi.PromptBuildRequest{
		CurrentMessage:       pluginMessageContextFromRecord(record),
		CurrentSystemPrompt:  strings.TrimSpace(systemPrompt),
		CurrentPersonaPrompt: strings.TrimSpace(personaPrompt),
		Summary:              strings.TrimSpace(summary),
		Retrieved:            append([]string(nil), retrieved...),
		Recent:               history,
	})
	if err != nil {
		log.Printf("plugin prompt build error: %v", err)
		return nil
	}
	return blocks
}

func (h *Handler) postprocessPluginResponse(ctx context.Context, record memory.MessageRecord, response string) string {
	if h == nil || h.pluginManager == nil || strings.TrimSpace(response) == "" {
		return strings.TrimSpace(response)
	}

	postprocessed, err := h.pluginManager.PostprocessResponse(ctx, pluginapi.ResponsePostprocessRequest{
		CurrentMessage: pluginMessageContextFromRecord(record),
		Response:       strings.TrimSpace(response),
	})
	if err != nil {
		log.Printf("plugin response postprocess error: %v", err)
		return strings.TrimSpace(response)
	}
	return strings.TrimSpace(postprocessed)
}

func DefaultTimeout() time.Duration {
	return 45 * time.Second
}

func takeMemoryContents(items []memory.VectorRecord, limit int) []string {
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	contents := make([]string, 0, len(items))
	for _, item := range items {
		contents = append(contents, item.Content)
	}
	return contents
}

func takeRenderedMemories(items []memory.VectorRecord, limit int) []string {
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	rendered := make([]string, 0, len(items))
	for _, item := range items {
		rendered = append(rendered, item.Rendered)
	}
	return rendered
}

func matchRenderedMemories(candidates []memory.VectorRecord, rerankedContents []string, limit int) []string {
	if len(rerankedContents) == 0 {
		return takeRenderedMemories(candidates, limit)
	}

	indexed := make(map[string][]string, len(candidates))
	for _, candidate := range candidates {
		indexed[candidate.Content] = append(indexed[candidate.Content], candidate.Rendered)
	}

	rendered := make([]string, 0, len(rerankedContents))
	for _, content := range rerankedContents {
		items := indexed[content]
		if len(items) == 0 {
			continue
		}
		rendered = append(rendered, items[0])
		indexed[content] = items[1:]
		if limit > 0 && len(rendered) >= limit {
			break
		}
	}
	if len(rendered) == 0 {
		return takeRenderedMemories(candidates, limit)
	}
	return rendered
}

func splitNameAndBody(input string) (string, string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", false
	}
	index := strings.IndexAny(input, " \t\r\n")
	if index < 0 {
		return "", "", false
	}
	name := strings.TrimSpace(input[:index])
	body := strings.TrimSpace(input[index+1:])
	if name == "" || body == "" {
		return "", "", false
	}
	return name, body, true
}

func extractUserID(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false
	}
	field := strings.Fields(input)[0]
	if strings.HasPrefix(field, "<@") && strings.HasSuffix(field, ">") {
		field = strings.TrimSuffix(strings.TrimPrefix(field, "<@"), ">")
		field = strings.TrimPrefix(field, "!")
	}
	field = strings.TrimSpace(field)
	if field == "" {
		return "", false
	}
	return field, true
}

func renderIDList(ids []string) string {
	if len(ids) == 0 {
		return "- 无"
	}
	copyIDs := append([]string(nil), ids...)
	sort.Strings(copyIDs)
	return "- " + strings.Join(copyIDs, "\n- ")
}

func permissionDenied() string {
	return "你没有权限执行这个命令。"
}

func superAdminDenied() string {
	return "只有超级管理员可以执行这个命令。"
}

func commandHelp() string {
	parts := []string{
		"可用 Slash 命令:",
		setupHelp(),
		pluginHelp(),
		systemHelp(),
		adminHelp(),
	}
	return strings.Join(parts, "\n")
}

func personaHelp() string {
	return "/persona 打开一站式人设管理面板"
}

func setupHelp() string {
	return "/setup 打开允许发言范围管理面板"
}

func systemHelp() string {
	return strings.Join([]string{
		"/system show",
		"/system set prompt:<prompt>",
		"/system clear",
	}, "\n")
}

func emojiHelp() string {
	return "/emoji 打开服务器表情管理面板"
}

func proactiveHelp() string {
	return "/proactive 打开主动回复管理面板"
}

func pluginHelp() string {
	return "/plugin 打开插件管理面板"
}

func adminHelp() string {
	return strings.Join([]string{
		"/admin list",
		"/admin add user:<@user>",
		"/admin remove user:<@user>",
	}, "\n")
}

var errRuntimeStoreMissing = errors.New("runtime store is not configured")

func (h *Handler) ensureRuntimeStore() error {
	if h.runtimeStore == nil {
		return errRuntimeStoreMissing
	}
	return nil
}

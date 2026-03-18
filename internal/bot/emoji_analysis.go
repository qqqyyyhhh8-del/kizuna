package bot

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"kizuna/internal/openai"
	"kizuna/internal/runtimecfg"

	"github.com/bwmarrin/discordgo"
)

const (
	emojiBatchSize      = 16
	emojiSheetColumns   = 4
	emojiSheetRows      = 4
	emojiSheetCellSize  = 144
	emojiSheetPadding   = 16
	emojiSheetOuterGap  = 20
	emojiHTTPStatusBase = 300
)

type guildEmojiListFn func(guildID string) ([]*discordgo.Emoji, error)

func (h *Handler) EmojiAnalysisEdit(ctx context.Context, authorID, guildID, guildName, customID string, listFn guildEmojiListFn) (*discordgo.WebhookEdit, error) {
	if err := h.ensureRuntimeStore(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(guildID) == "" {
		return h.emojiPanelEdit(authorID, guildID, guildName, "表情管理只能在服务器频道中使用。")
	}
	if !h.runtimeStore.IsAdmin(authorID) {
		return h.emojiPanelEdit(authorID, guildID, guildName, permissionDenied())
	}
	if listFn == nil {
		return h.emojiPanelEdit(authorID, guildID, guildName, "当前无法读取服务器表情列表。")
	}
	if !h.beginEmojiAnalysis(guildID) {
		return h.emojiPanelEdit(authorID, guildID, guildName, "当前服务器的表情分析任务已在运行，请稍后刷新面板。")
	}
	defer h.finishEmojiAnalysis(guildID)

	emojis, err := listFn(guildID)
	if err != nil {
		return h.emojiPanelEdit(authorID, guildID, guildName, "读取服务器表情失败: "+err.Error())
	}

	notice, err := h.analyzeGuildEmojiWorldBook(ctx, guildID, guildName, authorID, emojis, strings.TrimSpace(customID) == emojiActionAnalyzeFull)
	if err != nil {
		return h.emojiPanelEdit(authorID, guildID, guildName, emojiAnalysisErrorMessage(err))
	}
	return h.emojiPanelEdit(authorID, guildID, guildName, notice)
}

func (h *Handler) beginEmojiAnalysis(guildID string) bool {
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return false
	}

	h.emojiMu.Lock()
	defer h.emojiMu.Unlock()

	if _, ok := h.emojiAnalyzing[guildID]; ok {
		return false
	}
	h.emojiAnalyzing[guildID] = struct{}{}
	return true
}

func (h *Handler) finishEmojiAnalysis(guildID string) {
	h.emojiMu.Lock()
	defer h.emojiMu.Unlock()

	delete(h.emojiAnalyzing, strings.TrimSpace(guildID))
}

func (h *Handler) isEmojiAnalyzing(guildID string) bool {
	h.emojiMu.Lock()
	defer h.emojiMu.Unlock()

	_, ok := h.emojiAnalyzing[strings.TrimSpace(guildID)]
	return ok
}

func (h *Handler) analyzeGuildEmojiWorldBook(ctx context.Context, guildID, guildName, authorID string, emojis []*discordgo.Emoji, forceFull bool) (string, error) {
	guildID = strings.TrimSpace(guildID)
	guildName = strings.TrimSpace(guildName)
	authorID = strings.TrimSpace(authorID)

	profile, hadProfile := h.runtimeStore.GuildEmojiProfile(guildID)
	assets := collectGuildEmojiAssets(emojis)
	currentIDs := emojiAssetIDs(assets)
	worldBookKey := emojiWorldBookKey(guildID)
	timestamp := emojiAnalysisTimestamp()

	if len(assets) == 0 {
		if err := h.runtimeStore.DeleteWorldBookEntry(worldBookKey); err != nil {
			return "", err
		}
		if err := h.runtimeStore.UpsertGuildEmojiProfile(runtimecfg.GuildEmojiProfile{
			GuildID:         guildID,
			GuildName:       guildName,
			EmojiIDs:        nil,
			Summary:         "",
			WorldBookKey:    worldBookKey,
			LastAnalyzedAt:  timestamp,
			LastAnalyzedBy:  authorID,
			LastAnalyzeMode: "空服务器",
		}); err != nil {
			return "", err
		}
		return "当前服务器没有可分析的自定义表情，已清空对应世界书。", nil
	}

	removedCount := 0
	previousIDs := make(map[string]struct{}, len(profile.EmojiIDs))
	for _, oldID := range profile.EmojiIDs {
		previousIDs[oldID] = struct{}{}
	}
	currentSet := make(map[string]struct{}, len(currentIDs))
	for _, currentID := range currentIDs {
		currentSet[currentID] = struct{}{}
	}
	for oldID := range previousIDs {
		if _, ok := currentSet[oldID]; !ok {
			removedCount++
		}
	}

	modeLabel := "增量分析"
	existingSummary := ""
	targetAssets := assets
	autoRebuilt := false

	switch {
	case forceFull:
		modeLabel = "完整重建"
	case !hadProfile || len(profile.EmojiIDs) == 0 || strings.TrimSpace(h.emojiWorldBookText(guildID, profile)) == "":
		modeLabel = "完整重建"
	case removedCount > 0:
		modeLabel = "完整重建"
		autoRebuilt = true
	default:
		existingSummary = strings.TrimSpace(h.emojiWorldBookText(guildID, profile))
		targetAssets = filterNewEmojiAssets(assets, previousIDs)
		if len(targetAssets) == 0 {
			if strings.TrimSpace(existingSummary) != "" {
				if err := h.runtimeStore.UpsertWorldBookEntry(worldBookKey, runtimecfg.WorldBookEntry{
					Title:     emojiWorldBookTitle(guildName),
					Content:   existingSummary,
					GuildID:   guildID,
					Source:    "emoji_analysis",
					UpdatedAt: timestamp,
				}); err != nil {
					return "", err
				}
			}
			if err := h.runtimeStore.UpsertGuildEmojiProfile(runtimecfg.GuildEmojiProfile{
				GuildID:         guildID,
				GuildName:       guildName,
				EmojiIDs:        currentIDs,
				Summary:         existingSummary,
				WorldBookKey:    worldBookKey,
				LastAnalyzedAt:  profile.LastAnalyzedAt,
				LastAnalyzedBy:  profile.LastAnalyzedBy,
				LastAnalyzeMode: profile.LastAnalyzeMode,
			}); err != nil {
				return "", err
			}
			return fmt.Sprintf("当前服务器没有新增自定义表情；已同步当前表情数量 %d。", len(currentIDs)), nil
		}
	}

	summary, err := h.generateGuildEmojiWorldBook(ctx, guildID, guildName, authorID, targetAssets, existingSummary, modeLabel)
	if err != nil {
		return "", err
	}

	if err := h.runtimeStore.UpsertWorldBookEntry(worldBookKey, runtimecfg.WorldBookEntry{
		Title:     emojiWorldBookTitle(guildName),
		Content:   summary,
		GuildID:   guildID,
		Source:    "emoji_analysis",
		UpdatedAt: timestamp,
	}); err != nil {
		return "", err
	}
	if err := h.runtimeStore.UpsertGuildEmojiProfile(runtimecfg.GuildEmojiProfile{
		GuildID:         guildID,
		GuildName:       guildName,
		EmojiIDs:        currentIDs,
		Summary:         summary,
		WorldBookKey:    worldBookKey,
		LastAnalyzedAt:  timestamp,
		LastAnalyzedBy:  authorID,
		LastAnalyzeMode: modeLabel,
	}); err != nil {
		return "", err
	}

	notice := fmt.Sprintf("已完成%s，本次处理 %d 个表情，当前服务器共 %d 个。", modeLabel, len(targetAssets), len(currentIDs))
	if autoRebuilt {
		notice += " 检测到旧表情被删除，已自动改为完整重建。"
	}
	return notice, nil
}

func (h *Handler) generateGuildEmojiWorldBook(ctx context.Context, guildID, guildName, authorID string, assets []emojiAsset, existingSummary, modeLabel string) (string, error) {
	messages := []openai.ChatMessage{
		{
			Role: "system",
			Content: strings.TrimSpace(`你是 Discord 服务器的表情管理 agent。
你的任务是根据给出的服务器自定义表情图组，编写一段适合注入 system prompt 的“世界书”文本。
要求：
1. 必须用简洁中文输出。
2. 必须保留每个表情的可直接发送语法，例如 <:name:id> 或 <a:name:id>。
3. 重点描述每个表情大致表达的情绪、语气、适用场景、在聊天里什么时候适合发。
4. 不确定时要明确写“可能”“猜测”“大致”等，不要伪装成确定结论。
5. 输出应适合长期注入 prompt，不要写分析过程，不要输出 JSON。`),
		},
		{
			Role: "user",
			Content: strings.Join([]string{
				"服务器名称: " + emojiGuildLabel(guildID, guildName),
				"服务器ID: " + guildID,
				"本次操作: " + strings.TrimSpace(modeLabel),
				"触发管理员ID: " + strings.TrimSpace(authorID),
				renderExistingEmojiSummary(existingSummary),
			}, "\n\n"),
		},
	}

	batches := chunkEmojiAssets(assets, emojiBatchSize)
	var appendedBatches int
	for batchIndex, batch := range batches {
		sheetDataURL, renderedAssets, err := h.renderEmojiBatchSheet(ctx, batch)
		if err != nil {
			return "", err
		}
		if len(renderedAssets) == 0 {
			continue
		}
		appendedBatches++

		lines := []string{
			fmt.Sprintf("第 %d / %d 组表情图。图内排布顺序为从左到右、从上到下。", batchIndex+1, len(batches)),
			"请结合这张图识别这些表情的大致用法：",
		}
		for index, asset := range renderedAssets {
			lines = append(lines, fmt.Sprintf("%d. %s (名称: %s, ID: %s)", index+1, asset.Syntax, asset.Name, asset.ID))
		}

		messages = append(messages, openai.ChatMessage{
			Role: "user",
			Parts: []openai.ChatContentPart{
				openai.TextPart(strings.Join(lines, "\n")),
				openai.ImageURLPart(sheetDataURL),
			},
		})
	}
	if appendedBatches == 0 {
		return "", fmt.Errorf("没有可提交给模型的表情图组")
	}

	messages = append(messages, openai.ChatMessage{
		Role: "user",
		Content: strings.TrimSpace(`现在请直接输出更新后的“服务器表情世界书”正文。
输出要求：
1. 先给一小段整体风格说明。
2. 再给“表情清单”，尽量覆盖所有本次看到的表情。
3. 每条都要保留表情语法，并解释适用语境。
4. 如果是增量分析，要把已有世界书里仍然有效的内容保留下来并融合。
5. 不要输出 JSON，不要加无关客套。`),
	})

	response, err := h.chatFn(ctx, messages)
	if err != nil {
		return "", err
	}

	response = strings.TrimSpace(response)
	if response == "" {
		return "", fmt.Errorf("模型没有返回表情世界书内容")
	}
	return response, nil
}

func (h *Handler) renderEmojiBatchSheet(ctx context.Context, assets []emojiAsset) (string, []emojiAsset, error) {
	width := emojiSheetColumns*emojiSheetCellSize + (emojiSheetColumns-1)*emojiSheetPadding + emojiSheetOuterGap*2
	height := emojiSheetRows*emojiSheetCellSize + (emojiSheetRows-1)*emojiSheetPadding + emojiSheetOuterGap*2

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{245, 243, 239, 255}}, image.Point{}, draw.Src)

	rendered := make([]emojiAsset, 0, len(assets))
	for _, asset := range assets {
		img, err := h.fetchEmojiImage(ctx, asset.URL)
		if err != nil {
			continue
		}

		index := len(rendered)
		col := index % emojiSheetColumns
		row := index / emojiSheetColumns
		cellMinX := emojiSheetOuterGap + col*(emojiSheetCellSize+emojiSheetPadding)
		cellMinY := emojiSheetOuterGap + row*(emojiSheetCellSize+emojiSheetPadding)
		cellRect := image.Rect(cellMinX, cellMinY, cellMinX+emojiSheetCellSize, cellMinY+emojiSheetCellSize)

		draw.Draw(canvas, cellRect, &image.Uniform{C: color.RGBA{255, 255, 255, 255}}, image.Point{}, draw.Src)

		bounds := img.Bounds()
		offsetX := cellRect.Min.X + (emojiSheetCellSize-bounds.Dx())/2
		offsetY := cellRect.Min.Y + (emojiSheetCellSize-bounds.Dy())/2
		target := image.Rect(offsetX, offsetY, offsetX+bounds.Dx(), offsetY+bounds.Dy())
		draw.Draw(canvas, target, img, bounds.Min, draw.Over)
		rendered = append(rendered, asset)
	}

	if len(rendered) == 0 {
		return "", nil, fmt.Errorf("无法下载任何表情图片")
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		return "", nil, err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes()), rendered, nil
}

func (h *Handler) fetchEmojiImage(ctx context.Context, url string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(url), nil)
	if err != nil {
		return nil, err
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= emojiHTTPStatusBase {
		return nil, fmt.Errorf("emoji image request failed with status %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	return img, nil
}

func collectGuildEmojiAssets(emojis []*discordgo.Emoji) []emojiAsset {
	assets := make([]emojiAsset, 0, len(emojis))
	seen := make(map[string]struct{}, len(emojis))
	for _, emoji := range emojis {
		if emoji == nil {
			continue
		}
		asset, ok := emojiAssetFromDiscord(emoji)
		if !ok {
			continue
		}
		if _, exists := seen[asset.ID]; exists {
			continue
		}
		seen[asset.ID] = struct{}{}
		assets = append(assets, asset)
	}

	sort.Slice(assets, func(i, j int) bool {
		if assets[i].Name == assets[j].Name {
			return assets[i].ID < assets[j].ID
		}
		return assets[i].Name < assets[j].Name
	})
	return assets
}

func filterNewEmojiAssets(assets []emojiAsset, knownIDs map[string]struct{}) []emojiAsset {
	if len(assets) == 0 {
		return nil
	}
	filtered := make([]emojiAsset, 0, len(assets))
	for _, asset := range assets {
		if _, ok := knownIDs[asset.ID]; ok {
			continue
		}
		filtered = append(filtered, asset)
	}
	return filtered
}

func chunkEmojiAssets(assets []emojiAsset, batchSize int) [][]emojiAsset {
	if batchSize <= 0 || len(assets) == 0 {
		return nil
	}
	chunks := make([][]emojiAsset, 0, (len(assets)+batchSize-1)/batchSize)
	for start := 0; start < len(assets); start += batchSize {
		end := start + batchSize
		if end > len(assets) {
			end = len(assets)
		}
		chunk := append([]emojiAsset(nil), assets[start:end]...)
		chunks = append(chunks, chunk)
	}
	return chunks
}

func emojiAssetIDs(assets []emojiAsset) []string {
	ids := make([]string, 0, len(assets))
	for _, asset := range assets {
		ids = append(ids, asset.ID)
	}
	return ids
}

func emojiWorldBookTitle(guildName string) string {
	guildName = strings.TrimSpace(guildName)
	if guildName == "" {
		return "服务器表情世界书"
	}
	return guildName + " 表情世界书"
}

func renderExistingEmojiSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "当前还没有已有世界书，请直接基于本次图组生成完整内容。"
	}
	return "当前已有世界书，请在保留有效内容的前提下合并新增表情：\n\n" + summary
}

func emojiAnalysisTimestamp() string {
	location := time.FixedZone("UTC+8", 8*60*60)
	return time.Now().In(location).Format("2006-01-02 15:04:05 UTC+8")
}

func emojiAnalysisErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "context deadline exceeded") {
		return "表情分析失败: 请求模型超时。可以稍后重试；如果你的 OpenAI 兼容站点响应较慢，可在 .env 里增加 `OPENAI_HTTP_TIMEOUT_SECONDS=600`。"
	}
	return "表情分析失败: " + err.Error()
}

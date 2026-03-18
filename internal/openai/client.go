package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"kizuna/internal/config"
)

type Client struct {
	chat       serviceConfig
	embed      serviceConfig
	rerank     serviceConfig
	httpClient *http.Client
}

type serviceConfig struct {
	baseURL string
	apiKey  string
	model   string
}

func NewClient(cfg config.OpenAIConfig) *Client {
	httpClient := &http.Client{}
	if cfg.HTTPTimeout > 0 {
		httpClient.Timeout = cfg.HTTPTimeout
	}

	return &Client{
		chat: serviceConfig{
			baseURL: strings.TrimRight(cfg.BaseURL, "/"),
			apiKey:  cfg.APIKey,
			model:   cfg.ChatModel,
		},
		embed: serviceConfig{
			baseURL: strings.TrimRight(cfg.EmbedBaseURL, "/"),
			apiKey:  cfg.EmbedAPIKey,
			model:   cfg.EmbedModel,
		},
		rerank: serviceConfig{
			baseURL: strings.TrimRight(cfg.RerankBaseURL, "/"),
			apiKey:  cfg.RerankAPIKey,
			model:   cfg.RerankModel,
		},
		httpClient: httpClient,
	}
}

type ChatMessage struct {
	Role    string            `json:"role"`
	Content string            `json:"-"`
	Parts   []ChatContentPart `json:"-"`
}

type ChatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *ChatImageURL `json:"image_url,omitempty"`
}

type ChatImageURL struct {
	URL string `json:"url"`
}

func TextPart(text string) ChatContentPart {
	return ChatContentPart{
		Type: "text",
		Text: strings.TrimSpace(text),
	}
}

func ImageURLPart(url string) ChatContentPart {
	url = strings.TrimSpace(url)
	if url == "" {
		return ChatContentPart{}
	}
	return ChatContentPart{
		Type:     "image_url",
		ImageURL: &ChatImageURL{URL: url},
	}
}

func (m ChatMessage) MarshalJSON() ([]byte, error) {
	content, hasParts := normalizeChatContent(m.Content, m.Parts)
	payload := struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}{
		Role:    strings.TrimSpace(m.Role),
		Content: strings.TrimSpace(m.Content),
	}
	if hasParts {
		payload.Content = content
	}
	return json.Marshal(payload)
}

func (m *ChatMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	m.Role = strings.TrimSpace(raw.Role)
	m.Content = ""
	m.Parts = nil

	content := bytes.TrimSpace(raw.Content)
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return nil
	}

	if content[0] == '"' {
		return json.Unmarshal(content, &m.Content)
	}

	var parts []ChatContentPart
	if err := json.Unmarshal(content, &parts); err != nil {
		return err
	}
	m.Parts = normalizeChatParts(parts)

	textParts := make([]string, 0, len(m.Parts))
	for _, part := range m.Parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			textParts = append(textParts, strings.TrimSpace(part.Text))
		}
	}
	m.Content = strings.Join(textParts, "\n")
	return nil
}

func normalizeChatContent(content string, parts []ChatContentPart) ([]ChatContentPart, bool) {
	normalized := normalizeChatParts(parts)
	if len(normalized) > 0 {
		return normalized, true
	}
	return nil, false
}

func normalizeChatParts(parts []ChatContentPart) []ChatContentPart {
	normalized := make([]ChatContentPart, 0, len(parts))
	for _, part := range parts {
		switch strings.TrimSpace(part.Type) {
		case "text":
			text := strings.TrimSpace(part.Text)
			if text == "" {
				continue
			}
			normalized = append(normalized, ChatContentPart{
				Type: "text",
				Text: text,
			})
		case "image_url":
			if part.ImageURL == nil || strings.TrimSpace(part.ImageURL.URL) == "" {
				continue
			}
			normalized = append(normalized, ChatContentPart{
				Type: "image_url",
				ImageURL: &ChatImageURL{
					URL: strings.TrimSpace(part.ImageURL.URL),
				},
			})
		}
	}
	return normalized
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
}

func (c *Client) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	payload := chatRequest{
		Model:    c.chat.model,
		Messages: messages,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chat.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.chat.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("chat request failed with status %s", resp.Status)
	}
	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("no choices returned")
	}
	return parsed.Choices[0].Message.Content, nil
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func (c *Client) Embed(ctx context.Context, input string) ([]float64, error) {
	payload := embedRequest{
		Model: c.embed.model,
		Input: []string{input},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.embed.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.embed.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding request failed with status %s", resp.Status)
	}
	var parsed embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) == 0 {
		return nil, errors.New("no embeddings returned")
	}
	return parsed.Data[0].Embedding, nil
}

type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

type rerankResult struct {
	Index int `json:"index"`
}

type rerankResponse struct {
	Results []rerankResult `json:"results"`
	Data    []rerankResult `json:"data"`
}

func (c *Client) CanRerank() bool {
	return c.rerank.baseURL != "" && c.rerank.apiKey != "" && c.rerank.model != ""
}

func (c *Client) Rerank(ctx context.Context, query string, documents []string, topN int) ([]string, error) {
	if !c.CanRerank() {
		return nil, errors.New("rerank is not configured")
	}
	if len(documents) == 0 {
		return nil, nil
	}
	if topN <= 0 || topN > len(documents) {
		topN = len(documents)
	}

	payload := rerankRequest{
		Model:     c.rerank.model,
		Query:     query,
		Documents: documents,
		TopN:      topN,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rerank.baseURL+"/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.rerank.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rerank request failed with status %s", resp.Status)
	}

	var parsed rerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	results := parsed.Results
	if len(results) == 0 {
		results = parsed.Data
	}
	if len(results) == 0 {
		return nil, errors.New("no rerank results returned")
	}

	reranked := make([]string, 0, len(results))
	for _, result := range results {
		if result.Index < 0 || result.Index >= len(documents) {
			continue
		}
		reranked = append(reranked, documents[result.Index])
		if len(reranked) >= topN {
			break
		}
	}
	if len(reranked) == 0 {
		return nil, errors.New("no valid rerank results returned")
	}
	return reranked, nil
}

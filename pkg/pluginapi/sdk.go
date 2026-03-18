package pluginapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
)

const (
	MethodPluginInitialize            = "plugin.initialize"
	MethodPluginShutdown              = "plugin.shutdown"
	MethodPluginOnSlashCommand        = "plugin.on_slash_command"
	MethodPluginOnComponent           = "plugin.on_component"
	MethodPluginOnModal               = "plugin.on_modal"
	MethodPluginOnMessage             = "plugin.on_message"
	MethodPluginOnContextBuild        = "plugin.on_context_build"
	MethodPluginOnPromptBuild         = "plugin.on_prompt_build"
	MethodPluginOnResponsePostprocess = "plugin.on_response_postprocess"
	MethodPluginOnReplyCommitted      = "plugin.on_reply_committed"
	MethodPluginOnInterval            = "plugin.on_interval"

	MethodHostStorageGet       = "host.storage.get"
	MethodHostStorageSet       = "host.storage.set"
	MethodHostStorageDelete    = "host.storage.delete"
	MethodHostStorageList      = "host.storage.list"
	MethodHostConfigGet        = "host.config.get"
	MethodHostConfigSet        = "host.config.set"
	MethodHostPersonaList      = "host.persona.list"
	MethodHostPersonaGetActive = "host.persona.get_active"
	MethodHostPersonaUpsert    = "host.persona.upsert"
	MethodHostPersonaDelete    = "host.persona.delete"
	MethodHostPersonaActivate  = "host.persona.activate"
	MethodHostPersonaClear     = "host.persona.clear_active"
	MethodHostRecordsGet       = "host.records.get"
	MethodHostRecordsPut       = "host.records.put"
	MethodHostRecordsDelete    = "host.records.delete"
	MethodHostRecordsList      = "host.records.list"
	MethodHostMemoryGet        = "host.memory.get"
	MethodHostMemorySearch     = "host.memory.search"
	MethodHostMemoryAppend     = "host.memory.append"
	MethodHostMemorySetSummary = "host.memory.set_summary"
	MethodHostMemoryTrim       = "host.memory.trim"
	MethodHostListGuildEmojis  = "host.discord.list_guild_emojis"
	MethodHostChat             = "host.chat"
	MethodHostEmbed            = "host.embed"
	MethodHostRerank           = "host.rerank"
	MethodHostSendMessage      = "host.send_message"
	MethodHostReplyToMessage   = "host.reply_to_message"
	MethodHostSpeechAllowed    = "host.speech.allowed"
	MethodHostGetWorldBook     = "host.worldbook.get"
	MethodHostUpsertWorldBook  = "host.worldbook.upsert"
	MethodHostDeleteWorldBook  = "host.worldbook.delete"
	MethodHostLog              = "host.log"
)

type Plugin interface {
	Initialize(ctx context.Context, host *HostClient, req InitializeRequest) error
	Shutdown(ctx context.Context, host *HostClient, req ShutdownRequest) error
	OnSlashCommand(ctx context.Context, host *HostClient, req SlashCommandRequest) (*InteractionResponse, error)
	OnComponent(ctx context.Context, host *HostClient, req ComponentRequest) (*InteractionResponse, error)
	OnModal(ctx context.Context, host *HostClient, req ModalRequest) (*InteractionResponse, error)
	OnMessage(ctx context.Context, host *HostClient, req MessageEvent) error
	OnContextBuild(ctx context.Context, host *HostClient, req ContextBuildRequest) (*ContextBuildResponse, error)
	OnPromptBuild(ctx context.Context, host *HostClient, req PromptBuildRequest) (*PromptBuildResponse, error)
	OnResponsePostprocess(ctx context.Context, host *HostClient, req ResponsePostprocessRequest) (*ResponsePostprocessResponse, error)
	OnReplyCommitted(ctx context.Context, host *HostClient, req ReplyCommittedRequest) error
	OnInterval(ctx context.Context, host *HostClient, req IntervalRequest) error
}

type BasePlugin struct{}

func (BasePlugin) Initialize(context.Context, *HostClient, InitializeRequest) error {
	return nil
}

func (BasePlugin) Shutdown(context.Context, *HostClient, ShutdownRequest) error {
	return nil
}

func (BasePlugin) OnSlashCommand(context.Context, *HostClient, SlashCommandRequest) (*InteractionResponse, error) {
	return nil, nil
}

func (BasePlugin) OnComponent(context.Context, *HostClient, ComponentRequest) (*InteractionResponse, error) {
	return nil, nil
}

func (BasePlugin) OnModal(context.Context, *HostClient, ModalRequest) (*InteractionResponse, error) {
	return nil, nil
}

func (BasePlugin) OnMessage(context.Context, *HostClient, MessageEvent) error {
	return nil
}

func (BasePlugin) OnContextBuild(context.Context, *HostClient, ContextBuildRequest) (*ContextBuildResponse, error) {
	return nil, nil
}

func (BasePlugin) OnPromptBuild(context.Context, *HostClient, PromptBuildRequest) (*PromptBuildResponse, error) {
	return nil, nil
}

func (BasePlugin) OnResponsePostprocess(context.Context, *HostClient, ResponsePostprocessRequest) (*ResponsePostprocessResponse, error) {
	return nil, nil
}

func (BasePlugin) OnReplyCommitted(context.Context, *HostClient, ReplyCommittedRequest) error {
	return nil
}

func (BasePlugin) OnInterval(context.Context, *HostClient, IntervalRequest) error {
	return nil
}

type HostClient struct {
	session *RPCSession
}

func NewHostClient(session *RPCSession) *HostClient {
	if session == nil {
		return nil
	}
	return &HostClient{session: session}
}

func (c *HostClient) StorageGet(ctx context.Context, key string, target any) (bool, error) {
	var response StorageGetResponse
	if err := c.session.Call(ctx, MethodHostStorageGet, StorageGetRequest{Key: key}, &response); err != nil {
		return false, err
	}
	if !response.Found || len(response.Value) == 0 || target == nil {
		return response.Found, nil
	}
	return true, json.Unmarshal(response.Value, target)
}

func (c *HostClient) StorageSet(ctx context.Context, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.session.Call(ctx, MethodHostStorageSet, StorageSetRequest{Key: key, Value: payload}, nil)
}

func (c *HostClient) StorageDelete(ctx context.Context, key string) error {
	return c.session.Call(ctx, MethodHostStorageDelete, StorageDeleteRequest{Key: key}, nil)
}

func (c *HostClient) StorageListKeys(ctx context.Context, prefix string) ([]string, error) {
	var response StorageListResponse
	if err := c.session.Call(ctx, MethodHostStorageList, StorageListRequest{Prefix: prefix}, &response); err != nil {
		return nil, err
	}
	return response.Keys, nil
}

func (c *HostClient) ConfigGet(ctx context.Context, target any) (bool, error) {
	var response ConfigGetResponse
	if err := c.session.Call(ctx, MethodHostConfigGet, struct{}{}, &response); err != nil {
		return false, err
	}
	if !response.Found || len(response.Value) == 0 || target == nil {
		return response.Found, nil
	}
	return true, json.Unmarshal(response.Value, target)
}

func (c *HostClient) ConfigSet(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.session.Call(ctx, MethodHostConfigSet, ConfigSetRequest{Value: payload}, nil)
}

func (c *HostClient) PersonaList(ctx context.Context, scope PersonaScope) (*PersonaListResponse, error) {
	var response PersonaListResponse
	if err := c.session.Call(ctx, MethodHostPersonaList, PersonaListRequest{Scope: scope}, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *HostClient) PersonaGetActive(ctx context.Context, scope PersonaScope) (*PersonaGetActiveResponse, error) {
	var response PersonaGetActiveResponse
	if err := c.session.Call(ctx, MethodHostPersonaGetActive, PersonaGetActiveRequest{Scope: scope}, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *HostClient) PersonaUpsert(ctx context.Context, request PersonaUpsertRequest) error {
	return c.session.Call(ctx, MethodHostPersonaUpsert, request, nil)
}

func (c *HostClient) PersonaDelete(ctx context.Context, scope PersonaScope, name string) error {
	return c.session.Call(ctx, MethodHostPersonaDelete, PersonaDeleteRequest{
		Scope: scope,
		Name:  name,
	}, nil)
}

func (c *HostClient) PersonaActivate(ctx context.Context, scope PersonaScope, name string) error {
	return c.session.Call(ctx, MethodHostPersonaActivate, PersonaActivateRequest{
		Scope: scope,
		Name:  name,
	}, nil)
}

func (c *HostClient) PersonaClearActive(ctx context.Context, scope PersonaScope) error {
	return c.session.Call(ctx, MethodHostPersonaClear, PersonaClearActiveRequest{Scope: scope}, nil)
}

func (c *HostClient) RecordsGet(ctx context.Context, collection, key string, target any) (bool, string, error) {
	var response RecordsGetResponse
	if err := c.session.Call(ctx, MethodHostRecordsGet, RecordsGetRequest{
		Collection: collection,
		Key:        key,
	}, &response); err != nil {
		return false, "", err
	}
	if !response.Found || len(response.Value) == 0 || target == nil {
		return response.Found, response.UpdatedAt, nil
	}
	return true, response.UpdatedAt, json.Unmarshal(response.Value, target)
}

func (c *HostClient) RecordsPut(ctx context.Context, collection, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.session.Call(ctx, MethodHostRecordsPut, RecordsPutRequest{
		Collection: collection,
		Key:        key,
		Value:      payload,
	}, nil)
}

func (c *HostClient) RecordsDelete(ctx context.Context, collection, key string) error {
	return c.session.Call(ctx, MethodHostRecordsDelete, RecordsDeleteRequest{
		Collection: collection,
		Key:        key,
	}, nil)
}

func (c *HostClient) RecordsList(ctx context.Context, request RecordsListRequest) (*RecordsListResponse, error) {
	var response RecordsListResponse
	if err := c.session.Call(ctx, MethodHostRecordsList, request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *HostClient) MemoryGet(ctx context.Context, channelID string) (*MemoryGetResponse, error) {
	var response MemoryGetResponse
	if err := c.session.Call(ctx, MethodHostMemoryGet, MemoryGetRequest{ChannelID: channelID}, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *HostClient) MemorySearch(ctx context.Context, channelID, query string, topN int) ([]MemorySearchResult, error) {
	var response MemorySearchResponse
	if err := c.session.Call(ctx, MethodHostMemorySearch, MemorySearchRequest{
		ChannelID: channelID,
		Query:     query,
		TopN:      topN,
	}, &response); err != nil {
		return nil, err
	}
	return response.Results, nil
}

func (c *HostClient) MemoryAppend(ctx context.Context, channelID string, message MemoryMessage) error {
	return c.session.Call(ctx, MethodHostMemoryAppend, MemoryAppendRequest{
		ChannelID: channelID,
		Message:   message,
	}, nil)
}

func (c *HostClient) MemorySetSummary(ctx context.Context, channelID, summary string) error {
	return c.session.Call(ctx, MethodHostMemorySetSummary, MemorySetSummaryRequest{
		ChannelID: channelID,
		Summary:   summary,
	}, nil)
}

func (c *HostClient) MemoryTrimHistory(ctx context.Context, channelID string, keep int) error {
	return c.session.Call(ctx, MethodHostMemoryTrim, MemoryTrimRequest{
		ChannelID: channelID,
		Keep:      keep,
	}, nil)
}

func (c *HostClient) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	var response ChatResponse
	if err := c.session.Call(ctx, MethodHostChat, ChatRequest{Messages: messages}, &response); err != nil {
		return "", err
	}
	return response.Content, nil
}

func (c *HostClient) ListGuildEmojis(ctx context.Context, guildID string) ([]GuildEmoji, error) {
	var response ListGuildEmojisResponse
	if err := c.session.Call(ctx, MethodHostListGuildEmojis, ListGuildEmojisRequest{GuildID: guildID}, &response); err != nil {
		return nil, err
	}
	return response.Emojis, nil
}

func (c *HostClient) Embed(ctx context.Context, input string) ([]float64, error) {
	var response EmbedResponse
	if err := c.session.Call(ctx, MethodHostEmbed, EmbedRequest{Input: input}, &response); err != nil {
		return nil, err
	}
	return response.Vector, nil
}

func (c *HostClient) Rerank(ctx context.Context, query string, documents []string, topN int) ([]string, error) {
	var response RerankResponse
	if err := c.session.Call(ctx, MethodHostRerank, RerankRequest{
		Query:     query,
		Documents: documents,
		TopN:      topN,
	}, &response); err != nil {
		return nil, err
	}
	return response.Documents, nil
}

func (c *HostClient) SendMessage(ctx context.Context, request SendMessageRequest) error {
	return c.session.Call(ctx, MethodHostSendMessage, request, nil)
}

func (c *HostClient) ReplyToMessage(ctx context.Context, message MessageContext) error {
	return c.session.Call(ctx, MethodHostReplyToMessage, ReplyToMessageRequest{Message: message}, nil)
}

func (c *HostClient) SpeechAllowed(ctx context.Context, guildID, channelID, threadID string) (bool, error) {
	var response SpeechAllowedResponse
	if err := c.session.Call(ctx, MethodHostSpeechAllowed, SpeechAllowedRequest{
		GuildID:   guildID,
		ChannelID: channelID,
		ThreadID:  threadID,
	}, &response); err != nil {
		return false, err
	}
	return response.Allowed, nil
}

func (c *HostClient) GetWorldBook(ctx context.Context, key string) (*GetWorldBookResponse, error) {
	var response GetWorldBookResponse
	if err := c.session.Call(ctx, MethodHostGetWorldBook, GetWorldBookRequest{Key: key}, &response); err != nil {
		return nil, err
	}
	if !response.Found {
		return nil, nil
	}
	return &response, nil
}

func (c *HostClient) UpsertWorldBook(ctx context.Context, request UpsertWorldBookRequest) error {
	return c.session.Call(ctx, MethodHostUpsertWorldBook, request, nil)
}

func (c *HostClient) DeleteWorldBook(ctx context.Context, key string) error {
	return c.session.Call(ctx, MethodHostDeleteWorldBook, DeleteWorldBookRequest{Key: key}, nil)
}

func (c *HostClient) Log(ctx context.Context, level, message string) error {
	return c.session.Call(ctx, MethodHostLog, LogRequest{Level: level, Message: message}, nil)
}

func Serve(manifest Manifest, plugin Plugin) error {
	manifest = manifest.Normalize()
	if err := manifest.Validate(); err != nil {
		return err
	}
	if plugin == nil {
		plugin = BasePlugin{}
	}

	session := NewRPCSession(os.Stdin, os.Stdout)
	host := &HostClient{session: session}

	session.RegisterHandler(MethodPluginInitialize, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request InitializeRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return struct{}{}, plugin.Initialize(ctx, host, request)
	})
	session.RegisterHandler(MethodPluginShutdown, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request ShutdownRequest
		if len(params) > 0 {
			if err := json.Unmarshal(params, &request); err != nil {
				return nil, err
			}
		}
		return struct{}{}, plugin.Shutdown(ctx, host, request)
	})
	session.RegisterHandler(MethodPluginOnSlashCommand, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request SlashCommandRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return plugin.OnSlashCommand(ctx, host, request)
	})
	session.RegisterHandler(MethodPluginOnComponent, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request ComponentRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return plugin.OnComponent(ctx, host, request)
	})
	session.RegisterHandler(MethodPluginOnModal, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request ModalRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return plugin.OnModal(ctx, host, request)
	})
	session.RegisterHandler(MethodPluginOnMessage, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request MessageEvent
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return struct{}{}, plugin.OnMessage(ctx, host, request)
	})
	session.RegisterHandler(MethodPluginOnContextBuild, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request ContextBuildRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return plugin.OnContextBuild(ctx, host, request)
	})
	session.RegisterHandler(MethodPluginOnPromptBuild, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request PromptBuildRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return plugin.OnPromptBuild(ctx, host, request)
	})
	session.RegisterHandler(MethodPluginOnResponsePostprocess, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request ResponsePostprocessRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return plugin.OnResponsePostprocess(ctx, host, request)
	})
	session.RegisterHandler(MethodPluginOnReplyCommitted, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request ReplyCommittedRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return struct{}{}, plugin.OnReplyCommitted(ctx, host, request)
	})
	session.RegisterHandler(MethodPluginOnInterval, func(ctx context.Context, params json.RawMessage) (any, error) {
		var request IntervalRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, err
		}
		return struct{}{}, plugin.OnInterval(ctx, host, request)
	})

	<-session.closed
	if errors.Is(session.closeErr, io.EOF) {
		return nil
	}
	return session.closeErr
}

package pluginapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Capability string

const (
	CapabilityDiscordReadMessage        Capability = "discord.read_message"
	CapabilityDiscordSendMessage        Capability = "discord.send_message"
	CapabilityDiscordReplyWithCore      Capability = "discord.reply_with_core"
	CapabilityDiscordReadGuildEmojis    Capability = "discord.read_guild_emojis"
	CapabilityDiscordInteractionRespond Capability = "discord.interaction.respond"
	CapabilityContextProvide            Capability = "context.provide"
	CapabilityLLMChat                   Capability = "llm.chat"
	CapabilityLLMEmbed                  Capability = "llm.embed"
	CapabilityLLMRerank                 Capability = "llm.rerank"
	CapabilityMemoryRead                Capability = "memory.read"
	CapabilityMemoryWrite               Capability = "memory.write"
	CapabilityPersonaRead               Capability = "persona.read"
	CapabilityPersonaWrite              Capability = "persona.write"
	CapabilityWorldBookRead             Capability = "worldbook.read"
	CapabilityWorldBookWrite            Capability = "worldbook.write"
	CapabilityPluginStorage             Capability = "plugin.storage"
	CapabilityPluginRecordsRead         Capability = "plugin.records.read"
	CapabilityPluginRecordsWrite        Capability = "plugin.records.write"
	CapabilityPluginConfigRead          Capability = "plugin.config.read"
	CapabilityPluginConfigWrite         Capability = "plugin.config.write"
)

type Manifest struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Version           string          `json:"version"`
	Description       string          `json:"description,omitempty"`
	MinHostVersion    string          `json:"min_host_version,omitempty"`
	Runtime           RuntimeSpec     `json:"runtime"`
	Capabilities      []Capability    `json:"capabilities,omitempty"`
	Dependencies      []Dependency    `json:"dependencies,omitempty"`
	Commands          []CommandSpec   `json:"commands,omitempty"`
	ComponentPrefixes []string        `json:"component_prefixes,omitempty"`
	IntervalSeconds   int             `json:"interval_seconds,omitempty"`
	ConfigSchema      json.RawMessage `json:"config_schema,omitempty"`
}

type Dependency struct {
	ID         string `json:"id"`
	MinVersion string `json:"min_version,omitempty"`
}

type RuntimeSpec struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type CommandSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Options     []CommandOption `json:"options,omitempty"`
}

type CommandOption struct {
	Type        CommandOptionType `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Required    bool              `json:"required,omitempty"`
	Options     []CommandOption   `json:"options,omitempty"`
}

type CommandOptionType string

const (
	CommandOptionTypeString     CommandOptionType = "string"
	CommandOptionTypeInteger    CommandOptionType = "integer"
	CommandOptionTypeNumber     CommandOptionType = "number"
	CommandOptionTypeBoolean    CommandOptionType = "boolean"
	CommandOptionTypeUser       CommandOptionType = "user"
	CommandOptionTypeChannel    CommandOptionType = "channel"
	CommandOptionTypeRole       CommandOptionType = "role"
	CommandOptionTypeSubcommand CommandOptionType = "subcommand"
)

func (m Manifest) Normalize() Manifest {
	m.ID = normalizeID(m.ID)
	m.Name = strings.TrimSpace(m.Name)
	m.Version = strings.TrimSpace(m.Version)
	m.Description = strings.TrimSpace(m.Description)
	m.MinHostVersion = strings.TrimSpace(m.MinHostVersion)
	m.Runtime.Command = strings.TrimSpace(m.Runtime.Command)
	m.Runtime.Args = normalizeArgs(m.Runtime.Args)
	m.Capabilities = normalizeCapabilities(m.Capabilities)
	m.Dependencies = normalizeDependencies(m.Dependencies)
	m.Commands = normalizeCommandSpecs(m.Commands)
	m.ComponentPrefixes = normalizeStrings(m.ComponentPrefixes)
	if m.IntervalSeconds < 0 {
		m.IntervalSeconds = 0
	}
	if len(m.ConfigSchema) == 0 {
		m.ConfigSchema = nil
	}
	return m
}

func (m Manifest) Validate() error {
	m = m.Normalize()
	switch {
	case m.ID == "":
		return errors.New("plugin id is required")
	case m.Name == "":
		return errors.New("plugin name is required")
	case m.Version == "":
		return errors.New("plugin version is required")
	case m.Runtime.Command == "":
		return errors.New("plugin runtime.command is required")
	}

	commandNames := make(map[string]struct{}, len(m.Commands))
	for _, command := range m.Commands {
		if strings.TrimSpace(command.Name) == "" {
			return errors.New("plugin command name is required")
		}
		if strings.TrimSpace(command.Description) == "" {
			return fmt.Errorf("plugin command %s description is required", command.Name)
		}
		if _, ok := commandNames[command.Name]; ok {
			return fmt.Errorf("duplicate plugin command: %s", command.Name)
		}
		commandNames[command.Name] = struct{}{}
		if err := validateCommandOptions(command.Options); err != nil {
			return fmt.Errorf("plugin command %s: %w", command.Name, err)
		}
	}

	prefixes := make(map[string]struct{}, len(m.ComponentPrefixes))
	for _, prefix := range m.ComponentPrefixes {
		if prefix == "" {
			return errors.New("plugin component prefix cannot be empty")
		}
		if _, ok := prefixes[prefix]; ok {
			return fmt.Errorf("duplicate component prefix: %s", prefix)
		}
		prefixes[prefix] = struct{}{}
	}
	dependencies := make(map[string]struct{}, len(m.Dependencies))
	for _, dependency := range m.Dependencies {
		if strings.TrimSpace(dependency.ID) == "" {
			return errors.New("plugin dependency id is required")
		}
		if dependency.ID == m.ID {
			return errors.New("plugin cannot depend on itself")
		}
		if _, ok := dependencies[dependency.ID]; ok {
			return fmt.Errorf("duplicate dependency: %s", dependency.ID)
		}
		dependencies[dependency.ID] = struct{}{}
	}
	return nil
}

func validateCommandOptions(options []CommandOption) error {
	names := make(map[string]struct{}, len(options))
	for _, option := range options {
		switch option.Type {
		case CommandOptionTypeString, CommandOptionTypeInteger, CommandOptionTypeNumber, CommandOptionTypeBoolean, CommandOptionTypeUser, CommandOptionTypeChannel, CommandOptionTypeRole, CommandOptionTypeSubcommand:
		default:
			return fmt.Errorf("unsupported command option type: %s", option.Type)
		}
		if strings.TrimSpace(option.Name) == "" {
			return errors.New("option name is required")
		}
		if strings.TrimSpace(option.Description) == "" {
			return fmt.Errorf("option %s description is required", option.Name)
		}
		if _, ok := names[option.Name]; ok {
			return fmt.Errorf("duplicate option: %s", option.Name)
		}
		names[option.Name] = struct{}{}
		if option.Type != CommandOptionTypeSubcommand && len(option.Options) > 0 {
			return fmt.Errorf("option %s only subcommands can have nested options", option.Name)
		}
		if option.Type == CommandOptionTypeSubcommand {
			if err := validateCommandOptions(option.Options); err != nil {
				return err
			}
		}
	}
	return nil
}

type HostInfo struct {
	Version   string `json:"version"`
	BotUserID string `json:"bot_user_id,omitempty"`
}

type GuildInfo struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type ChannelInfo struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`
	ThreadID   string `json:"thread_id,omitempty"`
	ThreadName string `json:"thread_name,omitempty"`
}

type UserInfo struct {
	ID           string `json:"id,omitempty"`
	Username     string `json:"username,omitempty"`
	GlobalName   string `json:"global_name,omitempty"`
	Nick         string `json:"nick,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	IsAdmin      bool   `json:"is_admin,omitempty"`
	IsSuperAdmin bool   `json:"is_super_admin,omitempty"`
}

type ReplyInfo struct {
	MessageID string   `json:"message_id,omitempty"`
	Role      string   `json:"role,omitempty"`
	Content   string   `json:"content,omitempty"`
	Time      string   `json:"time,omitempty"`
	Author    UserInfo `json:"author"`
}

type ImageReference struct {
	Kind        string `json:"kind,omitempty"`
	Name        string `json:"name,omitempty"`
	EmojiID     string `json:"emoji_id,omitempty"`
	URL         string `json:"url,omitempty"`
	Animated    bool   `json:"animated,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

type MessageContext struct {
	MessageID    string           `json:"message_id,omitempty"`
	Guild        GuildInfo        `json:"guild"`
	Channel      ChannelInfo      `json:"channel"`
	Content      string           `json:"content,omitempty"`
	Time         string           `json:"time,omitempty"`
	Author       UserInfo         `json:"author"`
	ReplyTo      *ReplyInfo       `json:"reply_to,omitempty"`
	Images       []ImageReference `json:"images,omitempty"`
	MentionedBot bool             `json:"mentioned_bot,omitempty"`
	RepliedToBot bool             `json:"replied_to_bot,omitempty"`
}

type MemoryMessage struct {
	Role    string           `json:"role"`
	Guild   GuildInfo        `json:"guild"`
	Content string           `json:"content,omitempty"`
	Time    string           `json:"time,omitempty"`
	Author  UserInfo         `json:"author"`
	ReplyTo *ReplyInfo       `json:"reply_to,omitempty"`
	Images  []ImageReference `json:"images,omitempty"`
}

type InitializeRequest struct {
	Host     HostInfo `json:"host"`
	Manifest Manifest `json:"manifest"`
}

type ShutdownRequest struct{}

type CommandOptionValue struct {
	Name         string               `json:"name"`
	Type         CommandOptionType    `json:"type"`
	StringValue  string               `json:"string_value,omitempty"`
	IntegerValue int64                `json:"integer_value,omitempty"`
	NumberValue  float64              `json:"number_value,omitempty"`
	BooleanValue bool                 `json:"boolean_value,omitempty"`
	UserID       string               `json:"user_id,omitempty"`
	ChannelID    string               `json:"channel_id,omitempty"`
	RoleID       string               `json:"role_id,omitempty"`
	Options      []CommandOptionValue `json:"options,omitempty"`
}

type SlashCommandRequest struct {
	PluginID    string               `json:"plugin_id"`
	CommandName string               `json:"command_name"`
	Guild       GuildInfo            `json:"guild"`
	Channel     ChannelInfo          `json:"channel"`
	User        UserInfo             `json:"user"`
	Options     []CommandOptionValue `json:"options,omitempty"`
}

type ComponentRequest struct {
	PluginID string      `json:"plugin_id"`
	CustomID string      `json:"custom_id"`
	Guild    GuildInfo   `json:"guild"`
	Channel  ChannelInfo `json:"channel"`
	User     UserInfo    `json:"user"`
	Values   []string    `json:"values,omitempty"`
}

type ModalRequest struct {
	PluginID string            `json:"plugin_id"`
	CustomID string            `json:"custom_id"`
	Guild    GuildInfo         `json:"guild"`
	Channel  ChannelInfo       `json:"channel"`
	User     UserInfo          `json:"user"`
	Fields   map[string]string `json:"fields,omitempty"`
}

type MessageEvent struct {
	PluginID string         `json:"plugin_id"`
	Message  MessageContext `json:"message"`
}

type ContextBuildRequest struct {
	PluginID             string                      `json:"plugin_id"`
	CurrentMessage       MessageContext              `json:"current_message"`
	CurrentSystemPrompt  string                      `json:"current_system_prompt,omitempty"`
	CurrentPersonaPrompt string                      `json:"current_persona_prompt,omitempty"`
	CurrentSummary       string                      `json:"current_summary,omitempty"`
	CurrentRetrieved     []string                    `json:"current_retrieved,omitempty"`
	CurrentRecent        []PromptConversationMessage `json:"current_recent,omitempty"`
}

type ContextBuildResponse struct {
	Override      bool                        `json:"override,omitempty"`
	SystemPrompt  string                      `json:"system_prompt,omitempty"`
	PersonaPrompt string                      `json:"persona_prompt,omitempty"`
	Summary       string                      `json:"summary,omitempty"`
	Retrieved     []string                    `json:"retrieved,omitempty"`
	Recent        []PromptConversationMessage `json:"recent,omitempty"`
	PromptBlocks  []PromptBlock               `json:"prompt_blocks,omitempty"`
}

type PromptConversationMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type PromptBuildRequest struct {
	PluginID             string                      `json:"plugin_id"`
	CurrentMessage       MessageContext              `json:"current_message"`
	CurrentSystemPrompt  string                      `json:"current_system_prompt,omitempty"`
	CurrentPersonaPrompt string                      `json:"current_persona_prompt,omitempty"`
	Summary              string                      `json:"summary,omitempty"`
	Retrieved            []string                    `json:"retrieved,omitempty"`
	Recent               []PromptConversationMessage `json:"recent,omitempty"`
}

type PromptBlock struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type PromptBuildResponse struct {
	Blocks []PromptBlock `json:"blocks,omitempty"`
}

type ResponsePostprocessRequest struct {
	PluginID       string         `json:"plugin_id"`
	CurrentMessage MessageContext `json:"current_message"`
	Response       string         `json:"response"`
}

type ResponsePostprocessResponse struct {
	Response string `json:"response"`
}

type ReplyCommittedRequest struct {
	PluginID       string         `json:"plugin_id"`
	TriggerMessage MessageContext `json:"trigger_message"`
	ReplyMessage   MessageContext `json:"reply_message"`
}

type IntervalRequest struct {
	PluginID string `json:"plugin_id"`
	FiredAt  string `json:"fired_at"`
}

type InteractionResponseType string

const (
	InteractionResponseTypeMessage     InteractionResponseType = "message"
	InteractionResponseTypeUpdate      InteractionResponseType = "update"
	InteractionResponseTypeModal       InteractionResponseType = "modal"
	InteractionResponseTypeDeferUpdate InteractionResponseType = "defer_update"
)

type InteractionResponse struct {
	Type    InteractionResponseType `json:"type"`
	Message *InteractionMessage     `json:"message,omitempty"`
	Modal   *ModalResponse          `json:"modal,omitempty"`
}

type InteractionMessage struct {
	Content    string      `json:"content,omitempty"`
	Ephemeral  bool        `json:"ephemeral,omitempty"`
	Embeds     []Embed     `json:"embeds,omitempty"`
	Components []ActionRow `json:"components,omitempty"`
}

type ModalResponse struct {
	CustomID string       `json:"custom_id"`
	Title    string       `json:"title"`
	Fields   []ModalField `json:"fields"`
}

type ModalField struct {
	CustomID    string `json:"custom_id"`
	Label       string `json:"label"`
	Style       string `json:"style,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Value       string `json:"value,omitempty"`
	Required    bool   `json:"required,omitempty"`
	MinLength   int    `json:"min_length,omitempty"`
	MaxLength   int    `json:"max_length,omitempty"`
}

type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      string       `json:"footer,omitempty"`
}

type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type ActionRow struct {
	Buttons     []Button     `json:"buttons,omitempty"`
	SelectMenus []SelectMenu `json:"select_menus,omitempty"`
}

type Button struct {
	CustomID string `json:"custom_id"`
	Label    string `json:"label"`
	Style    string `json:"style,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type SelectMenu struct {
	CustomID    string         `json:"custom_id"`
	Kind        string         `json:"kind,omitempty"`
	Placeholder string         `json:"placeholder,omitempty"`
	Options     []SelectOption `json:"options,omitempty"`
	MinValues   int            `json:"min_values,omitempty"`
	MaxValues   int            `json:"max_values,omitempty"`
	Disabled    bool           `json:"disabled,omitempty"`
}

type SelectOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
}

type StorageGetRequest struct {
	Key string `json:"key"`
}

type StorageGetResponse struct {
	Found bool            `json:"found"`
	Value json.RawMessage `json:"value,omitempty"`
}

type StorageSetRequest struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type StorageDeleteRequest struct {
	Key string `json:"key"`
}

type StorageListRequest struct {
	Prefix string `json:"prefix,omitempty"`
}

type StorageListResponse struct {
	Keys []string `json:"keys,omitempty"`
}

type ConfigGetResponse struct {
	Found bool            `json:"found"`
	Value json.RawMessage `json:"value,omitempty"`
}

type ConfigSetRequest struct {
	Value json.RawMessage `json:"value"`
}

type PersonaScope struct {
	Type      string `json:"type"`
	GuildID   string `json:"guild_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
}

const (
	PersonaScopeGlobal  = "global"
	PersonaScopeGuild   = "guild"
	PersonaScopeChannel = "channel"
	PersonaScopeThread  = "thread"
)

type PersonaEntry struct {
	Name      string `json:"name"`
	Prompt    string `json:"prompt"`
	Origin    string `json:"origin,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type PersonaListRequest struct {
	Scope PersonaScope `json:"scope"`
}

type PersonaListResponse struct {
	Active   string         `json:"active,omitempty"`
	Personas []PersonaEntry `json:"personas,omitempty"`
}

type PersonaGetActiveRequest struct {
	Scope PersonaScope `json:"scope"`
}

type PersonaGetActiveResponse struct {
	Found   bool         `json:"found"`
	Persona PersonaEntry `json:"persona,omitempty"`
}

type PersonaUpsertRequest struct {
	Scope  PersonaScope `json:"scope"`
	Name   string       `json:"name"`
	Prompt string       `json:"prompt"`
	Origin string       `json:"origin,omitempty"`
}

type PersonaDeleteRequest struct {
	Scope PersonaScope `json:"scope"`
	Name  string       `json:"name"`
}

type PersonaActivateRequest struct {
	Scope PersonaScope `json:"scope"`
	Name  string       `json:"name"`
}

type PersonaClearActiveRequest struct {
	Scope PersonaScope `json:"scope"`
}

type RecordsGetRequest struct {
	Collection string `json:"collection"`
	Key        string `json:"key"`
}

type RecordsGetResponse struct {
	Found     bool            `json:"found"`
	Value     json.RawMessage `json:"value,omitempty"`
	UpdatedAt string          `json:"updated_at,omitempty"`
}

type RecordsPutRequest struct {
	Collection string          `json:"collection"`
	Key        string          `json:"key"`
	Value      json.RawMessage `json:"value"`
}

type RecordsDeleteRequest struct {
	Collection string `json:"collection"`
	Key        string `json:"key"`
}

type RecordItem struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value,omitempty"`
	UpdatedAt string          `json:"updated_at,omitempty"`
}

type RecordsListRequest struct {
	Collection string `json:"collection"`
	Prefix     string `json:"prefix,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
}

type RecordsListResponse struct {
	Items      []RecordItem `json:"items,omitempty"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type MemoryGetRequest struct {
	ChannelID string `json:"channel_id"`
}

type MemoryGetResponse struct {
	Summary  string          `json:"summary,omitempty"`
	Messages []MemoryMessage `json:"messages,omitempty"`
}

type MemorySearchRequest struct {
	ChannelID string `json:"channel_id"`
	Query     string `json:"query"`
	TopN      int    `json:"top_n,omitempty"`
}

type MemorySearchResult struct {
	Content  string `json:"content"`
	Rendered string `json:"rendered,omitempty"`
	Time     string `json:"time,omitempty"`
}

type MemorySearchResponse struct {
	Results []MemorySearchResult `json:"results,omitempty"`
}

type MemoryAppendRequest struct {
	ChannelID string        `json:"channel_id"`
	Message   MemoryMessage `json:"message"`
}

type MemorySetSummaryRequest struct {
	ChannelID string `json:"channel_id"`
	Summary   string `json:"summary"`
}

type MemoryTrimRequest struct {
	ChannelID string `json:"channel_id"`
	Keep      int    `json:"keep"`
}

type GuildEmoji struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Animated bool   `json:"animated,omitempty"`
	URL      string `json:"url"`
	Syntax   string `json:"syntax,omitempty"`
}

type ListGuildEmojisRequest struct {
	GuildID string `json:"guild_id"`
}

type ListGuildEmojisResponse struct {
	Emojis []GuildEmoji `json:"emojis"`
}

type ChatMessage struct {
	Role    string            `json:"role"`
	Content string            `json:"content,omitempty"`
	Parts   []ChatContentPart `json:"parts,omitempty"`
}

type ChatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *ChatImageURL `json:"image_url,omitempty"`
}

type ChatImageURL struct {
	URL string `json:"url"`
}

type ChatRequest struct {
	Messages []ChatMessage `json:"messages"`
}

type ChatResponse struct {
	Content string `json:"content"`
}

type EmbedRequest struct {
	Input string `json:"input"`
}

type EmbedResponse struct {
	Vector []float64 `json:"vector"`
}

type RerankRequest struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
}

type RerankResponse struct {
	Documents []string `json:"documents"`
}

type SendMessageRequest struct {
	GuildID          string `json:"guild_id,omitempty"`
	ChannelID        string `json:"channel_id,omitempty"`
	ThreadID         string `json:"thread_id,omitempty"`
	Content          string `json:"content"`
	ReplyToMessageID string `json:"reply_to_message_id,omitempty"`
}

type UpsertWorldBookRequest struct {
	Key     string `json:"key"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
	GuildID string `json:"guild_id,omitempty"`
	Source  string `json:"source,omitempty"`
}

type GetWorldBookRequest struct {
	Key string `json:"key"`
}

type GetWorldBookResponse struct {
	Found   bool   `json:"found"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content,omitempty"`
	GuildID string `json:"guild_id,omitempty"`
	Source  string `json:"source,omitempty"`
}

type DeleteWorldBookRequest struct {
	Key string `json:"key"`
}

type ReplyToMessageRequest struct {
	Message MessageContext `json:"message"`
}

type SpeechAllowedRequest struct {
	GuildID   string `json:"guild_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
}

type SpeechAllowedResponse struct {
	Allowed bool `json:"allowed"`
}

type LogRequest struct {
	Level   string `json:"level,omitempty"`
	Message string `json:"message"`
}

func normalizeID(value string) string {
	return strings.TrimSpace(value)
}

func normalizeArgs(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		normalized = append(normalized, value)
	}
	return normalized
}

func normalizeCapabilities(values []Capability) []Capability {
	normalized := make([]Capability, 0, len(values))
	seen := map[Capability]struct{}{}
	for _, value := range values {
		value = Capability(strings.TrimSpace(string(value)))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i] < normalized[j]
	})
	return normalized
}

func normalizeStrings(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized
}

func normalizeCommandSpecs(values []CommandSpec) []CommandSpec {
	normalized := make([]CommandSpec, 0, len(values))
	for _, value := range values {
		value.Name = strings.TrimSpace(value.Name)
		value.Description = strings.TrimSpace(value.Description)
		value.Options = normalizeCommandOptions(value.Options)
		normalized = append(normalized, value)
	}
	return normalized
}

func normalizeCommandOptions(values []CommandOption) []CommandOption {
	normalized := make([]CommandOption, 0, len(values))
	for _, value := range values {
		value.Type = CommandOptionType(strings.TrimSpace(string(value.Type)))
		value.Name = strings.TrimSpace(value.Name)
		value.Description = strings.TrimSpace(value.Description)
		value.Options = normalizeCommandOptions(value.Options)
		normalized = append(normalized, value)
	}
	return normalized
}

func normalizeDependencies(values []Dependency) []Dependency {
	normalized := make([]Dependency, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value.ID = normalizeID(value.ID)
		value.MinVersion = strings.TrimSpace(value.MinVersion)
		if value.ID == "" {
			continue
		}
		if _, ok := seen[value.ID]; ok {
			continue
		}
		seen[value.ID] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].ID < normalized[j].ID
	})
	return normalized
}

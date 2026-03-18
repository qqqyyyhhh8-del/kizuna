package runtimecfg

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	sqlitestorage "kizuna/internal/storage/sqlite"
)

type Data struct {
	SuperAdminIDs      []string                     `json:"super_admin_ids"`
	AdminIDs           []string                     `json:"admin_ids"`
	Personas           map[string]string            `json:"personas,omitempty"`
	ActivePersona      string                       `json:"active_persona,omitempty"`
	SystemPrompt       string                       `json:"system_prompt"`
	SpeechMode         string                       `json:"speech_mode"`
	AllowedGuildIDs    []string                     `json:"allowed_guild_ids"`
	AllowedChannelIDs  []string                     `json:"allowed_channel_ids"`
	AllowedThreadIDs   []string                     `json:"allowed_thread_ids"`
	ProactiveReply     bool                         `json:"proactive_reply,omitempty"`
	ProactiveChance    float64                      `json:"proactive_chance,omitempty"`
	WorldBookEntries   map[string]WorldBookEntry    `json:"worldbook_entries"`
	GuildEmojiProfiles map[string]GuildEmojiProfile `json:"guild_emoji_profiles,omitempty"`
}

type WorldBookEntry struct {
	Title     string `json:"title"`
	Content   string `json:"content"`
	GuildID   string `json:"guild_id"`
	Source    string `json:"source"`
	UpdatedAt string `json:"updated_at"`
}

type GuildEmojiProfile struct {
	GuildID         string   `json:"guild_id"`
	GuildName       string   `json:"guild_name"`
	EmojiIDs        []string `json:"emoji_ids"`
	EmojiCount      int      `json:"emoji_count"`
	Summary         string   `json:"summary"`
	WorldBookKey    string   `json:"worldbook_key"`
	LastAnalyzedAt  string   `json:"last_analyzed_at"`
	LastAnalyzedBy  string   `json:"last_analyzed_by"`
	LastAnalyzeMode string   `json:"last_analyze_mode"`
}

const (
	SpeechModeAll       = "all"
	SpeechModeNone      = "none"
	SpeechModeAllowlist = "allowlist"
)

type flexibleIDs []string

func (ids *flexibleIDs) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*ids = nil
		return nil
	}

	var rawItems []json.RawMessage
	if err := json.Unmarshal(trimmed, &rawItems); err != nil {
		return err
	}

	parsed := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}

		if raw[0] == '"' {
			var id string
			if err := json.Unmarshal(raw, &id); err != nil {
				return err
			}
			parsed = append(parsed, id)
			continue
		}

		var number json.Number
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&number); err != nil {
			return err
		}
		parsed = append(parsed, number.String())
	}

	*ids = flexibleIDs(parsed)
	return nil
}

type Store struct {
	mu         sync.RWMutex
	db         *sql.DB
	data       Data
	legacyPath string
	ownsDB     bool

	configuredSuperAdminIDs []string
	configuredAdminIDs      []string
}

func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("config file path is required")
	}

	db, err := sqlitestorage.Open(defaultDBPathForLegacy(path))
	if err != nil {
		return nil, err
	}
	store := &Store{
		db:         db,
		legacyPath: path,
		ownsDB:     true,
	}
	if err := store.loadOrCreate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func OpenWithDB(db *sql.DB, legacyPath string) (*Store, error) {
	if db == nil {
		return nil, errors.New("sqlite db is required")
	}

	store := &Store{
		db:         db,
		legacyPath: strings.TrimSpace(legacyPath),
	}
	if err := store.loadOrCreate(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) loadOrCreate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureSchemaLocked(); err != nil {
		return err
	}

	if loaded, ok, err := s.loadFromSQLiteLocked(); err != nil {
		return err
	} else if ok {
		s.data = loaded
		if err := s.migrateLegacyPersonasLocked(); err != nil {
			return err
		}
		return s.loadConfiguredAdminsLocked()
	}

	if loaded, ok, err := loadLegacyData(s.legacyPath); err != nil {
		return err
	} else if ok {
		s.data = loaded
		if err := s.migrateLegacyPersonasLocked(); err != nil {
			return err
		}
		if err := s.persistLocked(); err != nil {
			return err
		}
		return s.loadConfiguredAdminsLocked()
	}

	s.data = defaultData()
	if err := s.migrateLegacyPersonasLocked(); err != nil {
		return err
	}
	if err := s.persistLocked(); err != nil {
		return err
	}
	return s.loadConfiguredAdminsLocked()
}

func (s *Store) ComposePrompts(baseSystemPrompt string) (string, string) {
	return s.ComposePromptsForLocation(baseSystemPrompt, "", "", "")
}

func (s *Store) ComposePromptsForGuild(baseSystemPrompt, guildID string) (string, string) {
	return s.ComposePromptsForLocation(baseSystemPrompt, guildID, "", "")
}

func (s *Store) ComposePromptsForLocation(baseSystemPrompt, guildID, channelID, threadID string) (string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	guildID = normalizeID(guildID)
	systemParts := make([]string, 0, 3)
	if base := strings.TrimSpace(baseSystemPrompt); base != "" {
		systemParts = append(systemParts, base)
	}
	if extra := strings.TrimSpace(s.data.SystemPrompt); extra != "" {
		systemParts = append(systemParts, extra)
	}
	if worldBook := composeWorldBookPrompt(s.data.WorldBookEntries, guildID); worldBook != "" {
		systemParts = append(systemParts, worldBook)
	}

	var personaPrompt string
	if active, ok, err := s.resolveActivePersonaLocked(PersonaScopeForLocation(guildID, channelID, threadID)); err == nil && ok {
		personaPrompt = strings.TrimSpace(active.Prompt)
	} else if active := strings.TrimSpace(s.data.ActivePersona); active != "" {
		personaPrompt = strings.TrimSpace(s.data.Personas[active])
	}

	return strings.Join(systemParts, "\n\n"), personaPrompt
}

func (s *Store) WorldBookEntriesForGuild(guildID string) map[string]WorldBookEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return copyWorldBookEntries(filterWorldBookEntries(s.data.WorldBookEntries, normalizeID(guildID)))
}

func (s *Store) UpsertWorldBookEntry(key string, entry WorldBookEntry) error {
	key = normalizeWorldBookKey(key)
	entry = normalizeWorldBookEntry(entry)
	if key == "" {
		return errors.New("世界书条目 key 不能为空")
	}
	if entry.Content == "" {
		return errors.New("世界书内容不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data.WorldBookEntries == nil {
		s.data.WorldBookEntries = map[string]WorldBookEntry{}
	}
	s.data.WorldBookEntries[key] = entry
	return s.persistLocked()
}

func (s *Store) DeleteWorldBookEntry(key string) error {
	key = normalizeWorldBookKey(key)
	if key == "" {
		return errors.New("世界书条目 key 不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data.WorldBookEntries, key)
	return s.persistLocked()
}

func (s *Store) GuildEmojiProfile(guildID string) (GuildEmojiProfile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	profile, ok := s.data.GuildEmojiProfiles[normalizeID(guildID)]
	if !ok {
		return GuildEmojiProfile{}, false
	}
	return normalizeGuildEmojiProfile(profile), true
}

func (s *Store) UpsertGuildEmojiProfile(profile GuildEmojiProfile) error {
	profile = normalizeGuildEmojiProfile(profile)
	if profile.GuildID == "" {
		return errors.New("服务器 ID 不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data.GuildEmojiProfiles == nil {
		s.data.GuildEmojiProfiles = map[string]GuildEmojiProfile{}
	}
	s.data.GuildEmojiProfiles[profile.GuildID] = profile
	return s.persistLocked()
}

func (s *Store) IsSuperAdmin(userID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	userID = normalizeID(userID)
	return containsString(s.configuredSuperAdminIDs, userID) || containsString(s.data.SuperAdminIDs, userID)
}

func (s *Store) IsAdmin(userID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	userID = normalizeID(userID)
	return containsString(s.configuredSuperAdminIDs, userID) ||
		containsString(s.configuredAdminIDs, userID) ||
		containsString(s.data.SuperAdminIDs, userID) ||
		containsString(s.data.AdminIDs, userID)
}

func (s *Store) AdminLists() ([]string, []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	superAdmins := normalizeIDs(append(append([]string(nil), s.configuredSuperAdminIDs...), s.data.SuperAdminIDs...))
	admins := normalizeIDs(append(append([]string(nil), s.configuredAdminIDs...), s.data.AdminIDs...))
	if len(superAdmins) > 0 && len(admins) > 0 {
		filtered := admins[:0]
		for _, adminID := range admins {
			if !containsString(superAdmins, adminID) {
				filtered = append(filtered, adminID)
			}
		}
		admins = filtered
	}
	return superAdmins, admins
}

func (s *Store) GrantAdmin(userID string) error {
	userID = normalizeID(userID)
	if userID == "" {
		return errors.New("管理员 ID 不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if containsString(s.configuredSuperAdminIDs, userID) || containsString(s.configuredAdminIDs, userID) ||
		containsString(s.data.SuperAdminIDs, userID) || containsString(s.data.AdminIDs, userID) {
		return s.persistLocked()
	}
	s.data.AdminIDs = append(s.data.AdminIDs, userID)
	sort.Strings(s.data.AdminIDs)
	return s.persistLocked()
}

func (s *Store) RevokeAdmin(userID string) error {
	userID = normalizeID(userID)
	if userID == "" {
		return errors.New("管理员 ID 不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if containsString(s.configuredSuperAdminIDs, userID) || containsString(s.data.SuperAdminIDs, userID) {
		return errors.New("超级管理员只能在配置文件里修改")
	}
	if containsString(s.configuredAdminIDs, userID) {
		return errors.New("配置文件中的管理员只能在配置文件里修改")
	}
	s.data.AdminIDs = removeString(s.data.AdminIDs, userID)
	return s.persistLocked()
}

func (s *Store) UpsertPersona(name, prompt string) error {
	return s.UpsertScopedPersona(GlobalPersonaScope(), name, prompt, "legacy")
}

func (s *Store) DeletePersona(name string) error {
	return s.DeleteScopedPersona(GlobalPersonaScope(), name)
}

func (s *Store) PersonaPrompt(name string) (string, bool) {
	persona, ok, err := s.ScopedPersona(GlobalPersonaScope(), name)
	if err != nil || !ok {
		return "", false
	}
	return persona.Prompt, true
}

func (s *Store) PersonaNames() []string {
	personas, _, err := s.ListScopedPersonas(GlobalPersonaScope())
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(personas))
	for _, persona := range personas {
		names = append(names, persona.Name)
	}
	return names
}

func (s *Store) ActivePersonaName() string {
	persona, ok, err := s.ActiveScopedPersona(GlobalPersonaScope())
	if err != nil || !ok {
		return ""
	}
	return persona.Name
}

func (s *Store) SetActivePersona(name string) error {
	return s.ActivateScopedPersona(GlobalPersonaScope(), name)
}

func (s *Store) ClearActivePersona() error {
	return s.ClearScopedPersonaActive(GlobalPersonaScope())
}

func (s *Store) SystemPrompt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.SystemPrompt
}

func (s *Store) SetSystemPrompt(prompt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.SystemPrompt = strings.TrimSpace(prompt)
	return s.persistLocked()
}

func (s *Store) SpeechScope() (string, []string, []string, []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.data.SpeechMode,
		append([]string(nil), s.data.AllowedGuildIDs...),
		append([]string(nil), s.data.AllowedChannelIDs...),
		append([]string(nil), s.data.AllowedThreadIDs...)
}

func (s *Store) ProactiveReplyConfig() (bool, float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.data.ProactiveReply, s.data.ProactiveChance
}

func (s *Store) SetProactiveReplyEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.ProactiveReply = enabled
	return s.persistLocked()
}

func (s *Store) SetProactiveReplyChance(chance float64) error {
	if chance < 0 || chance > 100 {
		return errors.New("主动回复概率必须在 0 到 100 之间")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.ProactiveChance = chance
	return s.persistLocked()
}

func (s *Store) SetSpeechMode(mode string) error {
	mode = normalizeSpeechMode(mode)
	if mode == "" {
		return errors.New("发言模式无效")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.SpeechMode = mode
	return s.persistLocked()
}

func (s *Store) SetAllowedGuildIDs(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.AllowedGuildIDs = normalizeIDs(ids)
	return s.persistLocked()
}

func (s *Store) AddAllowedGuildID(id string) error {
	id = normalizeID(id)
	if id == "" {
		return errors.New("服务器 ID 不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.SpeechMode = SpeechModeAllowlist
	s.data.AllowedGuildIDs = normalizeIDs(append(s.data.AllowedGuildIDs, id))
	return s.persistLocked()
}

func (s *Store) RemoveAllowedGuildID(id string) error {
	id = normalizeID(id)
	if id == "" {
		return errors.New("服务器 ID 不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.AllowedGuildIDs = normalizeIDs(removeString(s.data.AllowedGuildIDs, id))
	return s.persistLocked()
}

func (s *Store) SetAllowedChannelIDs(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.AllowedChannelIDs = normalizeIDs(ids)
	return s.persistLocked()
}

func (s *Store) AddAllowedChannelID(id string) error {
	id = normalizeID(id)
	if id == "" {
		return errors.New("频道 ID 不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.SpeechMode = SpeechModeAllowlist
	s.data.AllowedChannelIDs = normalizeIDs(append(s.data.AllowedChannelIDs, id))
	return s.persistLocked()
}

func (s *Store) RemoveAllowedChannelID(id string) error {
	id = normalizeID(id)
	if id == "" {
		return errors.New("频道 ID 不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.AllowedChannelIDs = normalizeIDs(removeString(s.data.AllowedChannelIDs, id))
	return s.persistLocked()
}

func (s *Store) SetAllowedThreadIDs(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.AllowedThreadIDs = normalizeIDs(ids)
	return s.persistLocked()
}

func (s *Store) AddAllowedThreadID(id string) error {
	id = normalizeID(id)
	if id == "" {
		return errors.New("子区 ID 不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.SpeechMode = SpeechModeAllowlist
	s.data.AllowedThreadIDs = normalizeIDs(append(s.data.AllowedThreadIDs, id))
	return s.persistLocked()
}

func (s *Store) RemoveAllowedThreadID(id string) error {
	id = normalizeID(id)
	if id == "" {
		return errors.New("子区 ID 不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.AllowedThreadIDs = normalizeIDs(removeString(s.data.AllowedThreadIDs, id))
	return s.persistLocked()
}

func (s *Store) AllowsSpeech(guildID, channelID, threadID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	guildID = normalizeID(guildID)
	channelID = normalizeID(channelID)
	threadID = normalizeID(threadID)

	switch s.data.SpeechMode {
	case SpeechModeAll:
		return true
	case SpeechModeNone:
		return false
	case SpeechModeAllowlist:
		if containsString(s.data.AllowedGuildIDs, guildID) {
			return true
		}
		if containsString(s.data.AllowedChannelIDs, channelID) {
			return true
		}
		if containsString(s.data.AllowedThreadIDs, threadID) {
			return true
		}
		return false
	default:
		return false
	}
}

func (s *Store) persistLocked() error {
	normalizeData(&s.data)
	if err := s.ensureSchemaLocked(); err != nil {
		return err
	}

	data, err := json.Marshal(s.data)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
INSERT INTO runtime_state (id, payload_json)
VALUES (1, ?)
ON CONFLICT(id) DO UPDATE SET payload_json = excluded.payload_json
	`, string(data))
	return err
}

func (s *Store) Close() error {
	if s == nil || !s.ownsDB || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) ensureSchemaLocked() error {
	if s == nil || s.db == nil {
		return errors.New("sqlite db is not initialized")
	}
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS runtime_state (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	payload_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS scoped_personas (
	scope_key TEXT NOT NULL,
	name TEXT NOT NULL,
	prompt TEXT NOT NULL,
	origin TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (scope_key, name)
);
CREATE TABLE IF NOT EXISTS scoped_persona_active (
	scope_key TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT ''
)
`)
	return err
}

func (s *Store) loadFromSQLiteLocked() (Data, bool, error) {
	var payload string
	err := s.db.QueryRow(`SELECT payload_json FROM runtime_state WHERE id = 1`).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return Data{}, false, nil
	}
	if err != nil {
		return Data{}, false, err
	}

	loaded, err := parseDataJSON([]byte(payload))
	if err != nil {
		return Data{}, false, err
	}
	return loaded, true, nil
}

func loadLegacyData(path string) (Data, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Data{}, false, nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Data{}, false, nil
	}
	if err != nil {
		return Data{}, false, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return Data{}, false, nil
	}

	loaded, err := parseDataJSON(data)
	if err != nil {
		return Data{}, false, err
	}
	return loaded, true, nil
}

func parseDataJSON(data []byte) (Data, error) {
	var parsed struct {
		SuperAdminIDs      flexibleIDs                  `json:"super_admin_ids"`
		AdminIDs           flexibleIDs                  `json:"admin_ids"`
		Personas           map[string]string            `json:"personas"`
		ActivePersona      string                       `json:"active_persona"`
		SystemPrompt       string                       `json:"system_prompt"`
		SpeechMode         string                       `json:"speech_mode"`
		AllowedGuildIDs    flexibleIDs                  `json:"allowed_guild_ids"`
		AllowedChannelIDs  flexibleIDs                  `json:"allowed_channel_ids"`
		AllowedThreadIDs   flexibleIDs                  `json:"allowed_thread_ids"`
		ProactiveReply     bool                         `json:"proactive_reply"`
		ProactiveChance    float64                      `json:"proactive_chance"`
		WorldBookEntries   map[string]WorldBookEntry    `json:"worldbook_entries"`
		GuildEmojiProfiles map[string]GuildEmojiProfile `json:"guild_emoji_profiles"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Data{}, err
	}

	loaded := Data{
		SuperAdminIDs:      []string(parsed.SuperAdminIDs),
		AdminIDs:           []string(parsed.AdminIDs),
		Personas:           parsed.Personas,
		ActivePersona:      parsed.ActivePersona,
		SystemPrompt:       parsed.SystemPrompt,
		SpeechMode:         parsed.SpeechMode,
		AllowedGuildIDs:    []string(parsed.AllowedGuildIDs),
		AllowedChannelIDs:  []string(parsed.AllowedChannelIDs),
		AllowedThreadIDs:   []string(parsed.AllowedThreadIDs),
		ProactiveReply:     parsed.ProactiveReply,
		ProactiveChance:    parsed.ProactiveChance,
		WorldBookEntries:   parsed.WorldBookEntries,
		GuildEmojiProfiles: parsed.GuildEmojiProfiles,
	}
	normalizeData(&loaded)
	return loaded, nil
}

func (s *Store) loadConfiguredAdminsLocked() error {
	if s == nil {
		return nil
	}
	configured, ok, err := loadOrCreateLegacyAdminConfig(s.legacyPath, s.data)
	if err != nil {
		return err
	}
	if !ok {
		s.configuredSuperAdminIDs = nil
		s.configuredAdminIDs = nil
		return nil
	}
	s.configuredSuperAdminIDs = configured.SuperAdminIDs
	s.configuredAdminIDs = configured.AdminIDs
	return nil
}

func loadOrCreateLegacyAdminConfig(path string, fallback Data) (Data, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Data{}, false, nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		template := defaultData()
		template.SuperAdminIDs = normalizeIDs(append(template.SuperAdminIDs, fallback.SuperAdminIDs...))
		template.AdminIDs = normalizeIDs(append(template.AdminIDs, fallback.AdminIDs...))
		templateData, marshalErr := json.MarshalIndent(template, "", "  ")
		if marshalErr != nil {
			return Data{}, false, marshalErr
		}
		templateData = append(templateData, '\n')
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Data{}, false, err
		}
		if err := os.WriteFile(path, templateData, 0o600); err != nil {
			return Data{}, false, err
		}
		return Data{
			SuperAdminIDs: template.SuperAdminIDs,
			AdminIDs:      template.AdminIDs,
		}, true, nil
	}
	if err != nil {
		return Data{}, false, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return Data{}, false, nil
	}

	loaded, err := parseDataJSON(data)
	if err != nil {
		return Data{}, false, err
	}
	return Data{
		SuperAdminIDs: loaded.SuperAdminIDs,
		AdminIDs:      loaded.AdminIDs,
	}, true, nil
}

func defaultDBPathForLegacy(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "bot.db"
	}
	ext := filepath.Ext(path)
	if ext == "" {
		return path + ".db"
	}
	return strings.TrimSuffix(path, ext) + ".db"
}

func defaultData() Data {
	return Data{
		SuperAdminIDs:      []string{},
		AdminIDs:           []string{},
		Personas:           map[string]string{},
		SpeechMode:         SpeechModeAllowlist,
		AllowedGuildIDs:    []string{},
		AllowedChannelIDs:  []string{},
		AllowedThreadIDs:   []string{},
		ProactiveReply:     false,
		ProactiveChance:    0,
		WorldBookEntries:   map[string]WorldBookEntry{},
		GuildEmojiProfiles: map[string]GuildEmojiProfile{},
	}
}

func normalizeData(data *Data) {
	data.SuperAdminIDs = normalizeIDs(data.SuperAdminIDs)
	data.AdminIDs = normalizeIDs(data.AdminIDs)
	if len(data.SuperAdminIDs) > 0 && len(data.AdminIDs) > 0 {
		filtered := make([]string, 0, len(data.AdminIDs))
		for _, adminID := range data.AdminIDs {
			if !containsString(data.SuperAdminIDs, adminID) {
				filtered = append(filtered, adminID)
			}
		}
		data.AdminIDs = filtered
	}
	data.SystemPrompt = strings.TrimSpace(data.SystemPrompt)
	data.ActivePersona = normalizeName(data.ActivePersona)
	data.SpeechMode = normalizeSpeechMode(data.SpeechMode)
	data.AllowedGuildIDs = normalizeIDs(data.AllowedGuildIDs)
	data.AllowedChannelIDs = normalizeIDs(data.AllowedChannelIDs)
	data.AllowedThreadIDs = normalizeIDs(data.AllowedThreadIDs)
	data.ProactiveChance = normalizeProbability(data.ProactiveChance)

	if data.Personas == nil {
		data.Personas = map[string]string{}
	}
	if data.WorldBookEntries == nil {
		data.WorldBookEntries = map[string]WorldBookEntry{}
	}
	if data.GuildEmojiProfiles == nil {
		data.GuildEmojiProfiles = map[string]GuildEmojiProfile{}
	}

	normalizedPersonas := make(map[string]string, len(data.Personas))
	for name, prompt := range data.Personas {
		name = normalizeName(name)
		prompt = strings.TrimSpace(prompt)
		if name == "" || prompt == "" {
			continue
		}
		normalizedPersonas[name] = prompt
	}
	data.Personas = normalizedPersonas

	if data.ActivePersona != "" {
		if _, ok := data.Personas[data.ActivePersona]; !ok {
			data.ActivePersona = ""
		}
	}

	normalizedWorldBook := make(map[string]WorldBookEntry, len(data.WorldBookEntries))
	for key, entry := range data.WorldBookEntries {
		key = normalizeWorldBookKey(key)
		entry = normalizeWorldBookEntry(entry)
		if key == "" || entry.Content == "" {
			continue
		}
		normalizedWorldBook[key] = entry
	}
	data.WorldBookEntries = normalizedWorldBook

	normalizedEmojiProfiles := make(map[string]GuildEmojiProfile, len(data.GuildEmojiProfiles))
	for guildID, profile := range data.GuildEmojiProfiles {
		if profile.GuildID == "" {
			profile.GuildID = guildID
		}
		profile = normalizeGuildEmojiProfile(profile)
		if profile.GuildID == "" {
			continue
		}
		normalizedEmojiProfiles[profile.GuildID] = profile
	}
	data.GuildEmojiProfiles = normalizedEmojiProfiles
}

func normalizeIDs(ids []string) []string {
	set := make(map[string]struct{}, len(ids))
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		id = normalizeID(id)
		if id == "" {
			continue
		}
		if _, ok := set[id]; ok {
			continue
		}
		set[id] = struct{}{}
		normalized = append(normalized, id)
	}
	sort.Strings(normalized)
	return normalized
}

func normalizeID(id string) string {
	return strings.TrimSpace(id)
}

func normalizeName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeSpeechMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", SpeechModeAll, SpeechModeAllowlist:
		return SpeechModeAllowlist
	case SpeechModeNone:
		return SpeechModeNone
	default:
		return ""
	}
}

func normalizeProbability(probability float64) float64 {
	switch {
	case probability < 0:
		return 0
	case probability > 100:
		return 100
	default:
		return probability
	}
}

func normalizeWorldBookKey(key string) string {
	return strings.TrimSpace(key)
}

func normalizeWorldBookEntry(entry WorldBookEntry) WorldBookEntry {
	entry.Title = strings.TrimSpace(entry.Title)
	entry.Content = strings.TrimSpace(entry.Content)
	entry.GuildID = normalizeID(entry.GuildID)
	entry.Source = strings.TrimSpace(entry.Source)
	entry.UpdatedAt = strings.TrimSpace(entry.UpdatedAt)
	return entry
}

func normalizeGuildEmojiProfile(profile GuildEmojiProfile) GuildEmojiProfile {
	profile.GuildID = normalizeID(profile.GuildID)
	profile.GuildName = strings.TrimSpace(profile.GuildName)
	profile.EmojiIDs = normalizeIDs(profile.EmojiIDs)
	profile.EmojiCount = len(profile.EmojiIDs)
	profile.Summary = strings.TrimSpace(profile.Summary)
	profile.WorldBookKey = normalizeWorldBookKey(profile.WorldBookKey)
	profile.LastAnalyzedAt = strings.TrimSpace(profile.LastAnalyzedAt)
	profile.LastAnalyzedBy = normalizeID(profile.LastAnalyzedBy)
	profile.LastAnalyzeMode = strings.TrimSpace(profile.LastAnalyzeMode)
	return profile
}

func filterWorldBookEntries(entries map[string]WorldBookEntry, guildID string) map[string]WorldBookEntry {
	filtered := make(map[string]WorldBookEntry)
	for key, entry := range entries {
		entry = normalizeWorldBookEntry(entry)
		if entry.Content == "" {
			continue
		}
		if entry.GuildID != "" && entry.GuildID != guildID {
			continue
		}
		filtered[key] = entry
	}
	return filtered
}

func copyWorldBookEntries(entries map[string]WorldBookEntry) map[string]WorldBookEntry {
	if len(entries) == 0 {
		return map[string]WorldBookEntry{}
	}
	cloned := make(map[string]WorldBookEntry, len(entries))
	for key, entry := range entries {
		cloned[key] = entry
	}
	return cloned
}

func composeWorldBookPrompt(entries map[string]WorldBookEntry, guildID string) string {
	filtered := filterWorldBookEntries(entries, guildID)
	if len(filtered) == 0 {
		return ""
	}

	keys := make([]string, 0, len(filtered))
	for key := range filtered {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := []string{"世界书（以下内容属于长期设定和服务器特有知识，回答时应遵守并适当使用）："}
	for index, key := range keys {
		entry := filtered[key]
		title := entry.Title
		if title == "" {
			title = key
		}

		header := fmt.Sprintf("条目 %d: %s", index+1, title)
		meta := make([]string, 0, 2)
		if entry.GuildID != "" {
			meta = append(meta, "guild="+entry.GuildID)
		}
		if entry.Source != "" {
			meta = append(meta, "source="+entry.Source)
		}
		if len(meta) > 0 {
			header += " [" + strings.Join(meta, ", ") + "]"
		}
		parts = append(parts, header+"\n"+entry.Content)
	}
	return strings.Join(parts, "\n\n")
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func removeString(items []string, target string) []string {
	filtered := items[:0]
	for _, item := range items {
		if item != target {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

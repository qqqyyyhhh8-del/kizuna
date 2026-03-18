package runtimecfg

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	PersonaScopeGlobal  = "global"
	PersonaScopeGuild   = "guild"
	PersonaScopeChannel = "channel"
	PersonaScopeThread  = "thread"
)

type PersonaScope struct {
	Type      string
	GuildID   string
	ChannelID string
	ThreadID  string
}

type ScopedPersona struct {
	Name      string
	Prompt    string
	Origin    string
	UpdatedAt string
}

func GlobalPersonaScope() PersonaScope {
	return PersonaScope{Type: PersonaScopeGlobal}
}

func PersonaScopeForLocation(guildID, channelID, threadID string) PersonaScope {
	scope := NormalizePersonaScope(PersonaScope{
		GuildID:   guildID,
		ChannelID: channelID,
		ThreadID:  threadID,
	})
	switch {
	case scope.ThreadID != "":
		scope.Type = PersonaScopeThread
	case scope.ChannelID != "":
		scope.Type = PersonaScopeChannel
	case scope.GuildID != "":
		scope.Type = PersonaScopeGuild
	default:
		scope.Type = PersonaScopeGlobal
	}
	return NormalizePersonaScope(scope)
}

func NormalizePersonaScope(scope PersonaScope) PersonaScope {
	scope.Type = strings.ToLower(strings.TrimSpace(scope.Type))
	scope.GuildID = normalizeID(scope.GuildID)
	scope.ChannelID = normalizeID(scope.ChannelID)
	scope.ThreadID = normalizeID(scope.ThreadID)
	switch scope.Type {
	case PersonaScopeThread:
		if scope.ThreadID == "" {
			return PersonaScope{}
		}
	case PersonaScopeChannel:
		if scope.ChannelID == "" {
			return PersonaScope{}
		}
		scope.ThreadID = ""
	case PersonaScopeGuild:
		if scope.GuildID == "" {
			return PersonaScope{}
		}
		scope.ChannelID = ""
		scope.ThreadID = ""
	case PersonaScopeGlobal:
		scope.GuildID = ""
		scope.ChannelID = ""
		scope.ThreadID = ""
	default:
		return PersonaScope{}
	}
	return scope
}

func personaScopeKey(scope PersonaScope) string {
	scope = NormalizePersonaScope(scope)
	switch scope.Type {
	case PersonaScopeThread:
		return fmt.Sprintf("thread:%s:%s:%s", scope.GuildID, scope.ChannelID, scope.ThreadID)
	case PersonaScopeChannel:
		return fmt.Sprintf("channel:%s:%s", scope.GuildID, scope.ChannelID)
	case PersonaScopeGuild:
		return "guild:" + scope.GuildID
	case PersonaScopeGlobal:
		return PersonaScopeGlobal
	default:
		return ""
	}
}

func (s *Store) UpsertScopedPersona(scope PersonaScope, name, prompt, origin string) error {
	scope = NormalizePersonaScope(scope)
	name = normalizeName(name)
	prompt = strings.TrimSpace(prompt)
	origin = strings.TrimSpace(origin)
	if personaScopeKey(scope) == "" {
		return errors.New("人设作用域无效")
	}
	if name == "" {
		return errors.New("人设名称不能为空")
	}
	if prompt == "" {
		return errors.New("人设 Prompt 不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
INSERT INTO scoped_personas (scope_key, name, prompt, origin, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(scope_key, name) DO UPDATE SET
	prompt = excluded.prompt,
	origin = excluded.origin,
	updated_at = excluded.updated_at
`, personaScopeKey(scope), name, prompt, origin, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) DeleteScopedPersona(scope PersonaScope, name string) error {
	scope = NormalizePersonaScope(scope)
	name = normalizeName(name)
	if personaScopeKey(scope) == "" {
		return errors.New("人设作用域无效")
	}
	if name == "" {
		return errors.New("人设名称不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM scoped_personas WHERE scope_key = ? AND name = ?`, personaScopeKey(scope), name)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return errors.New("人设不存在")
	}
	active, err := s.activePersonaNameLocked(scope)
	if err != nil {
		return err
	}
	if active == name {
		if _, err := s.db.Exec(`DELETE FROM scoped_persona_active WHERE scope_key = ?`, personaScopeKey(scope)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ScopedPersona(scope PersonaScope, name string) (ScopedPersona, bool, error) {
	scope = NormalizePersonaScope(scope)
	name = normalizeName(name)
	if personaScopeKey(scope) == "" || name == "" {
		return ScopedPersona{}, false, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.scopedPersonaLocked(scope, name)
}

func (s *Store) ListScopedPersonas(scope PersonaScope) ([]ScopedPersona, string, error) {
	scope = NormalizePersonaScope(scope)
	if personaScopeKey(scope) == "" {
		return nil, "", nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	personas, err := s.listScopedPersonasLocked(scope)
	if err != nil {
		return nil, "", err
	}
	active, err := s.activePersonaNameLocked(scope)
	if err != nil {
		return nil, "", err
	}
	return personas, active, nil
}

func (s *Store) ActiveScopedPersona(scope PersonaScope) (ScopedPersona, bool, error) {
	scope = NormalizePersonaScope(scope)
	if personaScopeKey(scope) == "" {
		return ScopedPersona{}, false, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.activeScopedPersonaLocked(scope)
}

func (s *Store) ActivateScopedPersona(scope PersonaScope, name string) error {
	scope = NormalizePersonaScope(scope)
	name = normalizeName(name)
	if personaScopeKey(scope) == "" {
		return errors.New("人设作用域无效")
	}
	if name == "" {
		return errors.New("人设名称不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok, err := s.scopedPersonaLocked(scope, name); err != nil {
		return err
	} else if !ok {
		return errors.New("人设不存在")
	}
	_, err := s.db.Exec(`
INSERT INTO scoped_persona_active (scope_key, name)
VALUES (?, ?)
ON CONFLICT(scope_key) DO UPDATE SET name = excluded.name
`, personaScopeKey(scope), name)
	return err
}

func (s *Store) ClearScopedPersonaActive(scope PersonaScope) error {
	scope = NormalizePersonaScope(scope)
	if personaScopeKey(scope) == "" {
		return errors.New("人设作用域无效")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM scoped_persona_active WHERE scope_key = ?`, personaScopeKey(scope))
	return err
}

func (s *Store) resolveActivePersonaLocked(scope PersonaScope) (ScopedPersona, bool, error) {
	for _, candidate := range personaScopeFallbacks(scope) {
		persona, ok, err := s.activeScopedPersonaLocked(candidate)
		if err != nil {
			return ScopedPersona{}, false, err
		}
		if ok {
			return persona, true, nil
		}
	}
	return ScopedPersona{}, false, nil
}

func (s *Store) activeScopedPersonaLocked(scope PersonaScope) (ScopedPersona, bool, error) {
	active, err := s.activePersonaNameLocked(scope)
	if err != nil || active == "" {
		return ScopedPersona{}, false, err
	}
	return s.scopedPersonaLocked(scope, active)
}

func (s *Store) activePersonaNameLocked(scope PersonaScope) (string, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM scoped_persona_active WHERE scope_key = ?`, personaScopeKey(scope)).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return normalizeName(name), nil
}

func (s *Store) scopedPersonaLocked(scope PersonaScope, name string) (ScopedPersona, bool, error) {
	var persona ScopedPersona
	err := s.db.QueryRow(`
SELECT name, prompt, origin, updated_at
FROM scoped_personas
WHERE scope_key = ? AND name = ?
`, personaScopeKey(scope), name).Scan(&persona.Name, &persona.Prompt, &persona.Origin, &persona.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ScopedPersona{}, false, nil
	}
	if err != nil {
		return ScopedPersona{}, false, err
	}
	persona = normalizeScopedPersona(persona)
	return persona, true, nil
}

func (s *Store) listScopedPersonasLocked(scope PersonaScope) ([]ScopedPersona, error) {
	rows, err := s.db.Query(`
SELECT name, prompt, origin, updated_at
FROM scoped_personas
WHERE scope_key = ?
ORDER BY name ASC
`, personaScopeKey(scope))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	personas := make([]ScopedPersona, 0)
	for rows.Next() {
		var persona ScopedPersona
		if err := rows.Scan(&persona.Name, &persona.Prompt, &persona.Origin, &persona.UpdatedAt); err != nil {
			return nil, err
		}
		personas = append(personas, normalizeScopedPersona(persona))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return personas, nil
}

func (s *Store) countScopedPersonasLocked(scope PersonaScope) (int, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM scoped_personas WHERE scope_key = ?`, personaScopeKey(scope)).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) migrateLegacyPersonasLocked() error {
	if len(s.data.Personas) == 0 && strings.TrimSpace(s.data.ActivePersona) == "" {
		return nil
	}

	global := GlobalPersonaScope()
	count, err := s.countScopedPersonasLocked(global)
	if err != nil {
		return err
	}
	if count == 0 {
		keys := make([]string, 0, len(s.data.Personas))
		for name := range s.data.Personas {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			prompt := strings.TrimSpace(s.data.Personas[name])
			if prompt == "" {
				continue
			}
			if _, err := s.db.Exec(`
INSERT INTO scoped_personas (scope_key, name, prompt, origin, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(scope_key, name) DO NOTHING
`, personaScopeKey(global), normalizeName(name), prompt, "legacy", time.Now().UTC().Format(time.RFC3339)); err != nil {
				return err
			}
		}
		if active := normalizeName(s.data.ActivePersona); active != "" {
			if _, ok, err := s.scopedPersonaLocked(global, active); err != nil {
				return err
			} else if ok {
				if _, err := s.db.Exec(`
INSERT INTO scoped_persona_active (scope_key, name)
VALUES (?, ?)
ON CONFLICT(scope_key) DO UPDATE SET name = excluded.name
`, personaScopeKey(global), active); err != nil {
					return err
				}
			}
		}
	}

	s.data.Personas = map[string]string{}
	s.data.ActivePersona = ""
	return nil
}

func personaScopeFallbacks(scope PersonaScope) []PersonaScope {
	scope = NormalizePersonaScope(scope)
	switch scope.Type {
	case PersonaScopeThread:
		return []PersonaScope{
			scope,
			{Type: PersonaScopeChannel, GuildID: scope.GuildID, ChannelID: scope.ChannelID},
			{Type: PersonaScopeGuild, GuildID: scope.GuildID},
			GlobalPersonaScope(),
		}
	case PersonaScopeChannel:
		return []PersonaScope{
			scope,
			{Type: PersonaScopeGuild, GuildID: scope.GuildID},
			GlobalPersonaScope(),
		}
	case PersonaScopeGuild:
		return []PersonaScope{
			scope,
			GlobalPersonaScope(),
		}
	case PersonaScopeGlobal:
		return []PersonaScope{scope}
	default:
		return []PersonaScope{GlobalPersonaScope()}
	}
}

func normalizeScopedPersona(persona ScopedPersona) ScopedPersona {
	persona.Name = normalizeName(persona.Name)
	persona.Prompt = strings.TrimSpace(persona.Prompt)
	persona.Origin = strings.TrimSpace(persona.Origin)
	persona.UpdatedAt = strings.TrimSpace(persona.UpdatedAt)
	return persona
}

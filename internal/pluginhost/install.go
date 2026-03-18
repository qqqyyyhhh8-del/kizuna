package pluginhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"kizuna/pkg/pluginapi"
)

func (m *Manager) InstallFromGit(ctx context.Context, repo, ref, sourcePath string) (InstalledPlugin, error) {
	manifest, repoDir, subdir, err := m.cloneManifest(ctx, repo, ref, sourcePath)
	if err != nil {
		return InstalledPlugin{}, err
	}
	if _, ok := m.registry.Get(manifest.ID); ok {
		_ = os.RemoveAll(repoDir)
		return InstalledPlugin{}, fmt.Errorf("plugin already installed: %s", manifest.ID)
	}
	if err := m.validateManifest(manifest, ""); err != nil {
		_ = os.RemoveAll(repoDir)
		return InstalledPlugin{}, err
	}
	finalDir := m.registry.RepoDirectory(manifest.ID)
	if _, err := os.Stat(finalDir); err == nil {
		_ = os.RemoveAll(repoDir)
		return InstalledPlugin{}, fmt.Errorf("plugin directory already exists: %s", finalDir)
	}
	if err := os.MkdirAll(filepath.Dir(finalDir), 0o755); err != nil {
		_ = os.RemoveAll(repoDir)
		return InstalledPlugin{}, err
	}
	if err := os.Rename(repoDir, finalDir); err != nil {
		_ = os.RemoveAll(repoDir)
		return InstalledPlugin{}, err
	}

	install := InstalledPlugin{
		ID:           manifest.ID,
		Name:         manifest.Name,
		Version:      manifest.Version,
		Description:  manifest.Description,
		Repo:         strings.TrimSpace(repo),
		Ref:          strings.TrimSpace(ref),
		SourcePath:   strings.Trim(strings.TrimSpace(sourcePath), "/"),
		RepoDir:      finalDir,
		PluginSubdir: subdir,
		Manifest:     manifest,
		GrantedCaps:  manifest.Capabilities,
		Enabled:      true,
		GuildMode:    GuildModeAll,
		Storage:      map[string]json.RawMessage{},
	}
	if err := m.registry.Upsert(install); err != nil {
		return InstalledPlugin{}, err
	}
	if _, err := m.startPlugin(install); err != nil {
		_ = m.registry.SetLastError(install.ID, err.Error())
	}
	_ = m.refreshCommands()
	return install, nil
}

func (m *Manager) UpgradeFromGit(ctx context.Context, pluginID, ref string) (InstalledPlugin, error) {
	current, ok := m.registry.Get(pluginID)
	if !ok {
		return InstalledPlugin{}, errors.New("plugin not found")
	}

	manifest, repoDir, subdir, err := m.cloneManifest(ctx, current.Repo, firstNonEmpty(strings.TrimSpace(ref), current.Ref), current.SourcePath)
	if err != nil {
		return InstalledPlugin{}, err
	}
	if manifest.ID != current.ID {
		_ = os.RemoveAll(repoDir)
		return InstalledPlugin{}, fmt.Errorf("plugin id mismatch: expected %s, got %s", current.ID, manifest.ID)
	}
	if err := m.validateManifest(manifest, current.ID); err != nil {
		_ = os.RemoveAll(repoDir)
		return InstalledPlugin{}, err
	}

	if err := m.stopPlugin(current.ID); err != nil {
		_ = os.RemoveAll(repoDir)
		return InstalledPlugin{}, err
	}
	finalDir := m.registry.RepoDirectory(current.ID)
	if oldDir := strings.TrimSpace(current.RepoDir); oldDir != "" {
		_ = os.RemoveAll(oldDir)
	}
	if err := os.MkdirAll(filepath.Dir(finalDir), 0o755); err != nil {
		_ = os.RemoveAll(repoDir)
		return InstalledPlugin{}, err
	}
	if err := os.Rename(repoDir, finalDir); err != nil {
		_ = os.RemoveAll(repoDir)
		return InstalledPlugin{}, err
	}

	current.Name = manifest.Name
	current.Version = manifest.Version
	current.Description = manifest.Description
	current.Ref = firstNonEmpty(strings.TrimSpace(ref), current.Ref)
	current.RepoDir = finalDir
	current.PluginSubdir = subdir
	current.Manifest = manifest
	current.GrantedCaps = manifest.Capabilities
	current.LastError = ""

	if err := m.registry.Upsert(current); err != nil {
		return InstalledPlugin{}, err
	}
	if current.Enabled {
		if _, err := m.startPlugin(current); err != nil {
			_ = m.registry.SetLastError(current.ID, err.Error())
		}
	}
	_ = m.refreshCommands()
	return current, nil
}

func (m *Manager) Remove(pluginID string) error {
	current, ok := m.registry.Get(pluginID)
	if !ok {
		return errors.New("plugin not found")
	}
	if err := m.stopPlugin(pluginID); err != nil {
		return err
	}
	if err := m.registry.Remove(pluginID); err != nil {
		return err
	}
	if strings.TrimSpace(current.RepoDir) != "" {
		_ = os.RemoveAll(current.RepoDir)
	}
	return m.refreshCommands()
}

func (m *Manager) EnableGlobal(pluginID string) error {
	if err := m.registry.SetEnabled(pluginID, true); err != nil {
		return err
	}
	plugin, _ := m.registry.Get(pluginID)
	if _, err := m.startPlugin(plugin); err != nil {
		_ = m.registry.SetLastError(pluginID, err.Error())
	}
	return m.refreshCommands()
}

func (m *Manager) DisableGlobal(pluginID string) error {
	if err := m.registry.SetEnabled(pluginID, false); err != nil {
		return err
	}
	if err := m.stopPlugin(pluginID); err != nil {
		return err
	}
	return m.refreshCommands()
}

func (m *Manager) AllowGuild(pluginID, guildID string) error {
	return m.registry.AllowGuild(pluginID, guildID)
}

func (m *Manager) DenyGuild(pluginID, guildID string) error {
	return m.registry.DenyGuild(pluginID, guildID)
}

func (m *Manager) Permissions(pluginID string) ([]pluginapi.Capability, error) {
	plugin, ok := m.registry.Get(pluginID)
	if !ok {
		return nil, errors.New("plugin not found")
	}
	return append([]pluginapi.Capability(nil), plugin.GrantedCaps...), nil
}

func (m *Manager) refreshCommands() error {
	m.mu.RLock()
	refresh := m.refreshCommandsFn
	m.mu.RUnlock()
	if refresh == nil {
		return nil
	}
	return refresh()
}

func (m *Manager) cloneManifest(ctx context.Context, repo, ref, sourcePath string) (pluginapi.Manifest, string, string, error) {
	repo = strings.TrimSpace(repo)
	ref = strings.TrimSpace(ref)
	sourcePath = strings.Trim(strings.TrimSpace(sourcePath), "/")
	if repo == "" {
		return pluginapi.Manifest{}, "", "", errors.New("plugin repo is required")
	}

	tmpDir, err := os.MkdirTemp(m.registry.TempDirectory(), "plugin-install-*")
	if err != nil {
		return pluginapi.Manifest{}, "", "", err
	}
	if err := runGit(ctx, "", "clone", repo, tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return pluginapi.Manifest{}, "", "", err
	}
	if ref != "" {
		if err := runGit(ctx, tmpDir, "checkout", ref); err != nil {
			_ = os.RemoveAll(tmpDir)
			return pluginapi.Manifest{}, "", "", err
		}
	}

	pluginRoot := tmpDir
	if sourcePath != "" {
		pluginRoot = filepath.Join(tmpDir, sourcePath)
	}
	manifestPath := filepath.Join(pluginRoot, pluginManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return pluginapi.Manifest{}, "", "", err
	}
	var manifest pluginapi.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		_ = os.RemoveAll(tmpDir)
		return pluginapi.Manifest{}, "", "", err
	}
	manifest = manifest.Normalize()
	if err := manifest.Validate(); err != nil {
		_ = os.RemoveAll(tmpDir)
		return pluginapi.Manifest{}, "", "", err
	}
	if err := ensureMinHostVersion(m.hostVersion, manifest.MinHostVersion); err != nil {
		_ = os.RemoveAll(tmpDir)
		return pluginapi.Manifest{}, "", "", err
	}

	return manifest, tmpDir, sourcePath, nil
}

func (m *Manager) validateManifest(manifest pluginapi.Manifest, excludingPluginID string) error {
	for _, command := range manifest.Commands {
		if _, ok := m.reservedCommands[command.Name]; ok {
			return fmt.Errorf("plugin command conflicts with core command: %s", command.Name)
		}
	}
	for _, plugin := range m.registry.List() {
		if plugin.ID == excludingPluginID {
			continue
		}
		for _, existing := range plugin.Manifest.Commands {
			for _, command := range manifest.Commands {
				if existing.Name == command.Name {
					return fmt.Errorf("plugin command conflicts with installed plugin %s: %s", plugin.ID, command.Name)
				}
			}
		}
		for _, existingPrefix := range plugin.Manifest.ComponentPrefixes {
			for _, prefix := range manifest.ComponentPrefixes {
				if prefixesConflict(existingPrefix, prefix) {
					return fmt.Errorf("plugin component prefix conflicts with installed plugin %s: %s", plugin.ID, prefix)
				}
			}
		}
	}
	for _, reserved := range m.reservedComponentPrefixes {
		for _, prefix := range manifest.ComponentPrefixes {
			if prefixesConflict(reserved, prefix) {
				return fmt.Errorf("plugin component prefix conflicts with core prefix: %s", prefix)
			}
		}
	}
	return nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, text)
	}
	return nil
}

func prefixesConflict(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	return strings.HasPrefix(left, right) || strings.HasPrefix(right, left)
}

func ensureMinHostVersion(current, required string) error {
	current = strings.TrimSpace(current)
	required = strings.TrimSpace(required)
	if required == "" || current == "" {
		return nil
	}
	currentParts, err := parseSemver(current)
	if err != nil {
		return nil
	}
	requiredParts, err := parseSemver(required)
	if err != nil {
		return fmt.Errorf("invalid plugin min_host_version: %s", required)
	}
	for index := range requiredParts {
		if currentParts[index] > requiredParts[index] {
			return nil
		}
		if currentParts[index] < requiredParts[index] {
			return fmt.Errorf("plugin requires host version %s or newer", required)
		}
	}
	return nil
}

func parseSemver(value string) ([3]int, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return [3]int{}, errors.New("invalid semver")
	}
	var parsed [3]int
	for index, part := range parts {
		number, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return [3]int{}, err
		}
		parsed[index] = number
	}
	return parsed, nil
}

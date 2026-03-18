package pluginhost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"discordbot/pkg/pluginapi"
)

func TestRegistryMigratesLegacyJSONAndPersistsToSQLite(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, registryFileName)
	legacy := `{
  "plugins": {
    "demo": {
      "id": "demo",
      "name": "Demo Plugin",
      "version": "v1.0.0",
      "repo": "https://example.com/demo.git",
      "repo_dir": "plugins/repos/demo",
      "manifest": {
        "id": "demo",
        "name": "Demo Plugin",
        "version": "v1.0.0"
      },
      "granted_capabilities": ["plugin.storage"],
      "enabled": true,
      "storage": {
        "state": {"enabled": true}
      }
    }
  }
}`
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy registry: %v", err)
	}

	registry, err := OpenRegistry(dir)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	plugin, ok := registry.Get("demo")
	if !ok {
		t.Fatal("expected migrated plugin")
	}
	if plugin.Name != "Demo Plugin" || !plugin.Enabled {
		t.Fatalf("unexpected plugin after migration: %#v", plugin)
	}
	value, ok := registry.StorageGet("demo", "state")
	if !ok {
		t.Fatal("expected migrated plugin storage")
	}
	if string(value) != `{"enabled": true}` {
		t.Fatalf("unexpected migrated storage: %s", value)
	}

	if err := registry.Upsert(InstalledPlugin{
		ID:          "extra",
		Name:        "Extra Plugin",
		Version:     "v1.0.0",
		Repo:        "https://example.com/extra.git",
		RepoDir:     "plugins/repos/extra",
		Manifest:    pluginapi.Manifest{ID: "extra", Name: "Extra Plugin", Version: "v1.0.0"},
		Enabled:     true,
		GrantedCaps: []pluginapi.Capability{pluginapi.CapabilityPluginStorage},
	}); err != nil {
		t.Fatalf("upsert plugin: %v", err)
	}
	if err := registry.StorageSet("extra", "config", json.RawMessage(`{"mode":"sqlite"}`)); err != nil {
		t.Fatalf("storage set: %v", err)
	}
	if err := registry.Close(); err != nil {
		t.Fatalf("close registry: %v", err)
	}

	reopened, err := OpenRegistry(dir)
	if err != nil {
		t.Fatalf("reopen registry: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	if _, ok := reopened.Get("extra"); !ok {
		t.Fatal("expected plugin to persist in sqlite")
	}
	value, ok = reopened.StorageGet("extra", "config")
	if !ok {
		t.Fatal("expected sqlite-backed plugin storage")
	}
	if string(value) != `{"mode":"sqlite"}` {
		t.Fatalf("unexpected persisted storage: %s", value)
	}
}

package pluginhost

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"kizuna/pkg/pluginapi"
)

func TestBuildConversationContextRequiresOverride(t *testing.T) {
	manager, err := NewManager(Config{PluginsDir: filepath.Join(t.TempDir(), "plugins")})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	plugin := InstalledPlugin{
		ID:       "context_demo",
		Name:     "Context Demo",
		Version:  "v0.1.0",
		Manifest: pluginapi.Manifest{ID: "context_demo", Name: "Context Demo", Version: "v0.1.0"},
		Enabled:  true,
		GrantedCaps: []pluginapi.Capability{
			pluginapi.CapabilityContextProvide,
		},
	}
	if err := manager.registry.Upsert(plugin); err != nil {
		t.Fatalf("upsert plugin: %v", err)
	}

	hostSession, pluginSession, cleanup := newRPCSessionPair(t)
	defer cleanup()

	manager.mu.Lock()
	manager.processes[plugin.ID] = &managedPlugin{install: plugin, session: hostSession}
	manager.mu.Unlock()

	pluginSession.RegisterHandler(pluginapi.MethodPluginOnContextBuild, func(ctx context.Context, params json.RawMessage) (any, error) {
		return &pluginapi.ContextBuildResponse{
			Override: false,
			Summary:  "should not be used",
		}, nil
	})

	response, err := manager.BuildConversationContext(context.Background(), pluginapi.ContextBuildRequest{
		CurrentMessage: pluginapi.MessageContext{
			Guild: pluginapi.GuildInfo{ID: "guild-1"},
		},
	})
	if err != nil {
		t.Fatalf("build conversation context: %v", err)
	}
	if response != nil {
		t.Fatalf("expected nil response when override is false, got %#v", response)
	}
}

func TestBuildConversationContextReturnsOverrideResponse(t *testing.T) {
	manager, err := NewManager(Config{PluginsDir: filepath.Join(t.TempDir(), "plugins")})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	plugin := InstalledPlugin{
		ID:       "context_demo",
		Name:     "Context Demo",
		Version:  "v0.1.0",
		Manifest: pluginapi.Manifest{ID: "context_demo", Name: "Context Demo", Version: "v0.1.0"},
		Enabled:  true,
		GrantedCaps: []pluginapi.Capability{
			pluginapi.CapabilityContextProvide,
		},
	}
	if err := manager.registry.Upsert(plugin); err != nil {
		t.Fatalf("upsert plugin: %v", err)
	}

	hostSession, pluginSession, cleanup := newRPCSessionPair(t)
	defer cleanup()

	manager.mu.Lock()
	manager.processes[plugin.ID] = &managedPlugin{install: plugin, session: hostSession}
	manager.mu.Unlock()

	pluginSession.RegisterHandler(pluginapi.MethodPluginOnContextBuild, func(ctx context.Context, params json.RawMessage) (any, error) {
		return &pluginapi.ContextBuildResponse{
			Override: true,
			Summary:  "memory active",
		}, nil
	})

	response, err := manager.BuildConversationContext(context.Background(), pluginapi.ContextBuildRequest{
		CurrentMessage: pluginapi.MessageContext{
			Guild: pluginapi.GuildInfo{ID: "guild-1"},
		},
	})
	if err != nil {
		t.Fatalf("build conversation context: %v", err)
	}
	if response == nil || response.Summary != "memory active" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

package pluginhost

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
	"time"

	"kizuna/internal/memory"
	"kizuna/internal/runtimecfg"
	"kizuna/pkg/pluginapi"
)

func TestRegisterHostHandlersSupportsPluginConfig(t *testing.T) {
	manager, err := NewManager(Config{PluginsDir: filepath.Join(t.TempDir(), "plugins")})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	plugin := InstalledPlugin{
		ID:          "demo",
		Name:        "Demo Plugin",
		Version:     "v0.1.0",
		Manifest:    pluginapi.Manifest{ID: "demo", Name: "Demo Plugin", Version: "v0.1.0"},
		Enabled:     true,
		GrantedCaps: []pluginapi.Capability{pluginapi.CapabilityPluginConfigRead, pluginapi.CapabilityPluginConfigWrite},
	}
	if err := manager.registry.Upsert(plugin); err != nil {
		t.Fatalf("upsert plugin: %v", err)
	}

	hostSession, pluginSession, cleanup := newRPCSessionPair(t)
	defer cleanup()

	manager.registerHostHandlers(&managedPlugin{install: plugin, session: hostSession})

	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostConfigSet, pluginapi.ConfigSetRequest{
		Value: json.RawMessage(`{"enabled":true,"threshold":3}`),
	}, nil); err != nil {
		t.Fatalf("config set: %v", err)
	}

	var response pluginapi.ConfigGetResponse
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostConfigGet, struct{}{}, &response); err != nil {
		t.Fatalf("config get: %v", err)
	}
	if !response.Found {
		t.Fatal("expected stored config")
	}
	if string(response.Value) != `{"enabled":true,"threshold":3}` {
		t.Fatalf("unexpected config payload: %s", response.Value)
	}
}

func TestRegisterHostHandlersSupportsStorageListAndDelete(t *testing.T) {
	manager, err := NewManager(Config{PluginsDir: filepath.Join(t.TempDir(), "plugins")})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	plugin := InstalledPlugin{
		ID:          "storage_demo",
		Name:        "Storage Demo",
		Version:     "v0.1.0",
		Manifest:    pluginapi.Manifest{ID: "storage_demo", Name: "Storage Demo", Version: "v0.1.0"},
		Enabled:     true,
		GrantedCaps: []pluginapi.Capability{pluginapi.CapabilityPluginStorage},
	}
	if err := manager.registry.Upsert(plugin); err != nil {
		t.Fatalf("upsert plugin: %v", err)
	}

	hostSession, pluginSession, cleanup := newRPCSessionPair(t)
	defer cleanup()

	manager.registerHostHandlers(&managedPlugin{install: plugin, session: hostSession})

	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostStorageSet, pluginapi.StorageSetRequest{
		Key:   "cache:first",
		Value: json.RawMessage(`{"seen":true}`),
	}, nil); err != nil {
		t.Fatalf("storage set first: %v", err)
	}
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostStorageSet, pluginapi.StorageSetRequest{
		Key:   "state:second",
		Value: json.RawMessage(`{"value":2}`),
	}, nil); err != nil {
		t.Fatalf("storage set second: %v", err)
	}

	var list pluginapi.StorageListResponse
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostStorageList, pluginapi.StorageListRequest{
		Prefix: "cache:",
	}, &list); err != nil {
		t.Fatalf("storage list: %v", err)
	}
	if len(list.Keys) != 1 || list.Keys[0] != "cache:first" {
		t.Fatalf("unexpected storage keys: %#v", list.Keys)
	}

	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostStorageDelete, pluginapi.StorageDeleteRequest{
		Key: "cache:first",
	}, nil); err != nil {
		t.Fatalf("storage delete: %v", err)
	}

	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostStorageList, pluginapi.StorageListRequest{}, &list); err != nil {
		t.Fatalf("storage list all: %v", err)
	}
	if len(list.Keys) != 1 || list.Keys[0] != "state:second" {
		t.Fatalf("unexpected storage keys after delete: %#v", list.Keys)
	}
}

func TestRegisterHostHandlersSupportsMemoryReadWrite(t *testing.T) {
	store := memory.NewStore(func(ctx context.Context, input string) ([]float64, error) {
		return []float64{1, 0, 0}, nil
	})
	t.Cleanup(func() { _ = store.Close() })

	manager, err := NewManager(Config{
		PluginsDir:  filepath.Join(t.TempDir(), "plugins"),
		MemoryStore: store,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	plugin := InstalledPlugin{
		ID:       "memory_demo",
		Name:     "Memory Demo",
		Version:  "v0.1.0",
		Manifest: pluginapi.Manifest{ID: "memory_demo", Name: "Memory Demo", Version: "v0.1.0"},
		Enabled:  true,
		GrantedCaps: []pluginapi.Capability{
			pluginapi.CapabilityMemoryRead,
			pluginapi.CapabilityMemoryWrite,
		},
	}
	if err := manager.registry.Upsert(plugin); err != nil {
		t.Fatalf("upsert plugin: %v", err)
	}

	hostSession, pluginSession, cleanup := newRPCSessionPair(t)
	defer cleanup()

	manager.registerHostHandlers(&managedPlugin{install: plugin, session: hostSession})

	appendRequest := pluginapi.MemoryAppendRequest{
		ChannelID: "channel-1",
		Message: pluginapi.MemoryMessage{
			Role:    "user",
			Content: "alpha memory",
			Time:    time.Now().UTC().Format(time.RFC3339),
			Author: pluginapi.UserInfo{
				ID:          "user-1",
				Username:    "user-1",
				DisplayName: "user-1",
			},
		},
	}
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostMemoryAppend, appendRequest, nil); err != nil {
		t.Fatalf("memory append: %v", err)
	}
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostMemoryAppend, pluginapi.MemoryAppendRequest{
		ChannelID: "channel-1",
		Message: pluginapi.MemoryMessage{
			Role:    "assistant",
			Content: "assistant memory",
			Time:    time.Now().UTC().Format(time.RFC3339),
			Author: pluginapi.UserInfo{
				ID:          "bot-1",
				Username:    "bot-1",
				DisplayName: "bot-1",
			},
		},
	}, nil); err != nil {
		t.Fatalf("second memory append: %v", err)
	}
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostMemorySetSummary, pluginapi.MemorySetSummaryRequest{
		ChannelID: "channel-1",
		Summary:   "summary text",
	}, nil); err != nil {
		t.Fatalf("memory set summary: %v", err)
	}

	var snapshot pluginapi.MemoryGetResponse
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostMemoryGet, pluginapi.MemoryGetRequest{
		ChannelID: "channel-1",
	}, &snapshot); err != nil {
		t.Fatalf("memory get: %v", err)
	}
	if snapshot.Summary != "summary text" {
		t.Fatalf("unexpected summary: %q", snapshot.Summary)
	}
	if len(snapshot.Messages) != 2 || snapshot.Messages[0].Content != "alpha memory" || snapshot.Messages[1].Content != "assistant memory" {
		t.Fatalf("unexpected memory snapshot: %#v", snapshot.Messages)
	}

	search := waitForMemorySearchResults(t, pluginSession, pluginapi.MemorySearchRequest{
		ChannelID: "channel-1",
		Query:     "alpha memory",
		TopN:      1,
	})
	if len(search.Results) != 1 || search.Results[0].Content != "alpha memory" {
		t.Fatalf("unexpected memory search results: %#v", search.Results)
	}

	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostMemoryTrim, pluginapi.MemoryTrimRequest{
		ChannelID: "channel-1",
		Keep:      1,
	}, nil); err != nil {
		t.Fatalf("memory trim: %v", err)
	}
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostMemoryGet, pluginapi.MemoryGetRequest{
		ChannelID: "channel-1",
	}, &snapshot); err != nil {
		t.Fatalf("memory get after trim: %v", err)
	}
	if len(snapshot.Messages) != 1 || snapshot.Messages[0].Content != "assistant memory" {
		t.Fatalf("expected trimmed memory messages, got %#v", snapshot.Messages)
	}
}

func TestRegisterHostHandlersSupportsPersonaAPI(t *testing.T) {
	runtimeStore, err := runtimecfg.Open(filepath.Join(t.TempDir(), "runtime.json"))
	if err != nil {
		t.Fatalf("open runtime store: %v", err)
	}
	t.Cleanup(func() { _ = runtimeStore.Close() })

	manager, err := NewManager(Config{
		PluginsDir:   filepath.Join(t.TempDir(), "plugins"),
		RuntimeStore: runtimeStore,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	plugin := InstalledPlugin{
		ID:       "persona_demo",
		Name:     "Persona Demo",
		Version:  "v0.1.0",
		Manifest: pluginapi.Manifest{ID: "persona_demo", Name: "Persona Demo", Version: "v0.1.0"},
		Enabled:  true,
		GrantedCaps: []pluginapi.Capability{
			pluginapi.CapabilityPersonaRead,
			pluginapi.CapabilityPersonaWrite,
		},
	}
	if err := manager.registry.Upsert(plugin); err != nil {
		t.Fatalf("upsert plugin: %v", err)
	}

	hostSession, pluginSession, cleanup := newRPCSessionPair(t)
	defer cleanup()
	manager.registerHostHandlers(&managedPlugin{install: plugin, session: hostSession})

	scope := pluginapi.PersonaScope{
		Type:      pluginapi.PersonaScopeThread,
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		ThreadID:  "thread-1",
	}
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostPersonaUpsert, pluginapi.PersonaUpsertRequest{
		Scope:  scope,
		Name:   "auto",
		Prompt: "you are the scoped persona",
		Origin: "test",
	}, nil); err != nil {
		t.Fatalf("persona upsert: %v", err)
	}
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostPersonaActivate, pluginapi.PersonaActivateRequest{
		Scope: scope,
		Name:  "auto",
	}, nil); err != nil {
		t.Fatalf("persona activate: %v", err)
	}

	var list pluginapi.PersonaListResponse
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostPersonaList, pluginapi.PersonaListRequest{
		Scope: scope,
	}, &list); err != nil {
		t.Fatalf("persona list: %v", err)
	}
	if list.Active != "auto" || len(list.Personas) != 1 || list.Personas[0].Prompt != "you are the scoped persona" {
		t.Fatalf("unexpected persona list response: %#v", list)
	}

	var active pluginapi.PersonaGetActiveResponse
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostPersonaGetActive, pluginapi.PersonaGetActiveRequest{
		Scope: scope,
	}, &active); err != nil {
		t.Fatalf("persona get active: %v", err)
	}
	if !active.Found || active.Persona.Name != "auto" {
		t.Fatalf("unexpected active persona response: %#v", active)
	}

	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostPersonaDelete, pluginapi.PersonaDeleteRequest{
		Scope: scope,
		Name:  "auto",
	}, nil); err != nil {
		t.Fatalf("persona delete: %v", err)
	}
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostPersonaGetActive, pluginapi.PersonaGetActiveRequest{
		Scope: scope,
	}, &active); err != nil {
		t.Fatalf("persona get active after delete: %v", err)
	}
	if active.Found {
		t.Fatalf("expected active persona to be cleared after delete: %#v", active)
	}
}

func TestRegisterHostHandlersSupportsRecordsAPI(t *testing.T) {
	manager, err := NewManager(Config{PluginsDir: filepath.Join(t.TempDir(), "plugins")})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	plugin := InstalledPlugin{
		ID:       "records_demo",
		Name:     "Records Demo",
		Version:  "v0.1.0",
		Manifest: pluginapi.Manifest{ID: "records_demo", Name: "Records Demo", Version: "v0.1.0"},
		Enabled:  true,
		GrantedCaps: []pluginapi.Capability{
			pluginapi.CapabilityPluginRecordsRead,
			pluginapi.CapabilityPluginRecordsWrite,
		},
	}
	if err := manager.registry.Upsert(plugin); err != nil {
		t.Fatalf("upsert plugin: %v", err)
	}

	hostSession, pluginSession, cleanup := newRPCSessionPair(t)
	defer cleanup()
	manager.registerHostHandlers(&managedPlugin{install: plugin, session: hostSession})

	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostRecordsPut, pluginapi.RecordsPutRequest{
		Collection: "state",
		Key:        "scope:a",
		Value:      json.RawMessage(`{"count":1}`),
	}, nil); err != nil {
		t.Fatalf("records put first: %v", err)
	}
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostRecordsPut, pluginapi.RecordsPutRequest{
		Collection: "state",
		Key:        "scope:b",
		Value:      json.RawMessage(`{"count":2}`),
	}, nil); err != nil {
		t.Fatalf("records put second: %v", err)
	}

	var get pluginapi.RecordsGetResponse
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostRecordsGet, pluginapi.RecordsGetRequest{
		Collection: "state",
		Key:        "scope:a",
	}, &get); err != nil {
		t.Fatalf("records get: %v", err)
	}
	if !get.Found || string(get.Value) != `{"count":1}` || get.UpdatedAt == "" {
		t.Fatalf("unexpected records get response: %#v", get)
	}

	var list pluginapi.RecordsListResponse
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostRecordsList, pluginapi.RecordsListRequest{
		Collection: "state",
		Prefix:     "scope:",
		Limit:      10,
	}, &list); err != nil {
		t.Fatalf("records list: %v", err)
	}
	if len(list.Items) != 2 || list.Items[0].Key != "scope:a" || list.Items[1].Key != "scope:b" {
		t.Fatalf("unexpected records list response: %#v", list)
	}

	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostRecordsDelete, pluginapi.RecordsDeleteRequest{
		Collection: "state",
		Key:        "scope:a",
	}, nil); err != nil {
		t.Fatalf("records delete: %v", err)
	}
	if err := pluginSession.Call(context.Background(), pluginapi.MethodHostRecordsList, pluginapi.RecordsListRequest{
		Collection: "state",
		Limit:      10,
	}, &list); err != nil {
		t.Fatalf("records list after delete: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Key != "scope:b" {
		t.Fatalf("unexpected records list after delete: %#v", list)
	}
}

func waitForMemorySearchResults(t *testing.T, session *pluginapi.RPCSession, request pluginapi.MemorySearchRequest) pluginapi.MemorySearchResponse {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		var response pluginapi.MemorySearchResponse
		if err := session.Call(context.Background(), pluginapi.MethodHostMemorySearch, request, &response); err != nil {
			t.Fatalf("memory search: %v", err)
		}
		if len(response.Results) > 0 {
			return response
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for indexed memory search results")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func newRPCSessionPair(t *testing.T) (*pluginapi.RPCSession, *pluginapi.RPCSession, func()) {
	t.Helper()

	hostReader, pluginWriter := io.Pipe()
	pluginReader, hostWriter := io.Pipe()

	hostSession := pluginapi.NewRPCSession(hostReader, hostWriter)
	pluginSession := pluginapi.NewRPCSession(pluginReader, pluginWriter)

	cleanup := func() {
		hostSession.CloseWithError(io.EOF)
		pluginSession.CloseWithError(io.EOF)
		_ = hostReader.Close()
		_ = pluginReader.Close()
		_ = hostWriter.Close()
		_ = pluginWriter.Close()
	}
	return hostSession, pluginSession, cleanup
}

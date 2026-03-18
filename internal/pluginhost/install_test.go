package pluginhost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInstallFromGitReplacesOrphanedPluginDirectory(t *testing.T) {
	repoDir := createPluginGitRepo(t, "demo", "Demo Plugin")
	pluginsDir := filepath.Join(t.TempDir(), "plugins")

	manager, err := NewManager(Config{PluginsDir: pluginsDir, HostVersion: "v0.6.2"})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	orphanedDir := filepath.Join(pluginsDir, reposDirName, "demo")
	if err := os.MkdirAll(orphanedDir, 0o755); err != nil {
		t.Fatalf("mkdir orphaned dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orphanedDir, "stale.txt"), []byte("stale\n"), 0o600); err != nil {
		t.Fatalf("write orphaned file: %v", err)
	}

	installed, err := manager.InstallFromGit(context.Background(), repoDir, "", "")
	if err != nil {
		t.Fatalf("install from git: %v", err)
	}
	if installed.ID != "demo" {
		t.Fatalf("unexpected installed plugin id: %q", installed.ID)
	}
	if _, err := os.Stat(filepath.Join(orphanedDir, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected stale file to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(orphanedDir, pluginManifestFile)); err != nil {
		t.Fatalf("expected plugin manifest in final dir: %v", err)
	}
}

func createPluginGitRepo(t *testing.T, id, name string) string {
	t.Helper()

	repoDir := t.TempDir()
	manifest := `{
  "id": "` + id + `",
  "name": "` + name + `",
  "version": "v0.1.0",
  "runtime": {
    "command": "sh",
    "args": ["-c", "exit 0"]
  }
}`
	if err := os.WriteFile(filepath.Join(repoDir, pluginManifestFile), []byte(manifest+"\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	runGitCommand(t, repoDir, "init")
	runGitCommand(t, repoDir, "config", "user.email", "test@example.com")
	runGitCommand(t, repoDir, "config", "user.name", "Test User")
	runGitCommand(t, repoDir, "add", pluginManifestFile)
	runGitCommand(t, repoDir, "commit", "-m", "init")

	return repoDir
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

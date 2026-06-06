package source

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/John-Robertt/Auto-OpenWrt/internal/config"
	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

func TestUpdateClonesFetchesCleansAndWritesSnapshot(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	openwrtRepo := createGitRepo(t, "openwrt", map[string]string{"README.md": "one\n"})
	feedRepo := createGitRepo(t, "packages", map[string]string{"README.md": "feed\n"})
	pluginRepo := createGitRepo(t, "plugin", map[string]string{"luci-app-demo/Makefile": "include $(TOPDIR)/rules.mk\n"})
	manager := Manager{
		Store: store,
		Now:   func() time.Time { return time.Date(2026, 6, 7, 1, 2, 3, 0, time.UTC) },
	}

	input := UpdateInput{
		WorkspaceID: "auto-openwrt",
		RunID:       "20260607T010203Z-abc123",
		RunDir:      filepath.Join(root, "workspaces", "auto-openwrt", "runs", "update", "20260607T010203Z-abc123"),
		Plans: []config.SourceSetPlan{{
			SourceSetID: "src-abc123abc123",
			BuildIDs:    []string{"x86-64"},
			OpenWrt:     config.SourceRepository{Name: "openwrt", Repo: openwrtRepo, Branch: "main"},
			Feeds:       []config.SourceRepository{{Name: "packages", Repo: feedRepo, Branch: "main", Path: "feeds/packages"}},
			Plugins:     []config.PluginRepository{{Name: "demo", Type: "package", Repo: pluginRepo, Branch: "main", Path: "luci-app-demo"}},
		}},
	}

	result, err := manager.Update(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshots) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(result.Snapshots))
	}
	snapshotPath := filepath.Join(root, "sources", "source-sets", "src-abc123abc123", "source-set.json")
	assertJSONFile(t, snapshotPath)
	if result.SummaryPath == "" {
		t.Fatalf("summary path is empty")
	}
	if result.Snapshots[0].Plugins[0].Risk != "luci-app" {
		t.Fatalf("plugin risk = %s, want luci-app", result.Snapshots[0].Plugins[0].Risk)
	}

	cachePath := filepath.Join(root, "sources", "source-sets", "src-abc123abc123", "openwrt")
	if err := os.WriteFile(filepath.Join(cachePath, "dirty.tmp"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	updateGitRepo(t, openwrtRepo, map[string]string{"README.md": "two\n"})

	second, err := manager.Update(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cachePath, "dirty.tmp")); !os.IsNotExist(err) {
		t.Fatalf("dirty file still exists: %v", err)
	}
	if second.Snapshots[0].OpenWrt.Commit == result.Snapshots[0].OpenWrt.Commit {
		t.Fatalf("commit was not updated")
	}
}

func TestUpdateReturnsRepositoryError(t *testing.T) {
	store, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := Manager{Store: store}

	_, err = manager.Update(context.Background(), UpdateInput{
		WorkspaceID: "auto-openwrt",
		RunID:       "20260607T010203Z-abc123",
		RunDir:      filepath.Join(store.Root, "workspaces", "auto-openwrt", "runs", "update", "20260607T010203Z-abc123"),
		Plans: []config.SourceSetPlan{{
			SourceSetID: "src-fail00000000",
			BuildIDs:    []string{"x86-64"},
			OpenWrt:     config.SourceRepository{Name: "openwrt", Repo: filepath.Join(store.Root, "missing.git"), Branch: "main"},
		}},
	})
	var repoErr *RepositoryError
	if err == nil || !strings.Contains(err.Error(), "openwrt") {
		t.Fatalf("error = %v", err)
	}
	if !asRepositoryError(err, &repoErr) || repoErr.Name != "openwrt" || repoErr.Repo == "" {
		t.Fatalf("repository error = %#v", err)
	}
}

func TestDetectPluginRisk(t *testing.T) {
	root := t.TempDir()
	if got := DetectPluginRisk(config.PluginRepository{Name: "x", Type: "patch"}, root); got != "patch" {
		t.Fatalf("patch risk = %s", got)
	}
	if got := DetectPluginRisk(config.PluginRepository{Name: "luci-app-x", Type: "package"}, root); got != "luci-app" {
		t.Fatalf("luci risk = %s", got)
	}
	makefileDir := filepath.Join(root, "kmod")
	if err := os.MkdirAll(makefileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(makefileDir, "Makefile"), []byte("define KernelPackage/demo\nendef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DetectPluginRisk(config.PluginRepository{Name: "demo", Type: "package", Path: "kmod"}, root); got != "kernel-module" {
		t.Fatalf("kernel risk = %s", got)
	}
	if got := DetectPluginRisk(config.PluginRepository{Name: "demo", Type: "package", Risk: "unknown"}, root); got != "unknown" {
		t.Fatalf("explicit unknown risk = %s", got)
	}
}

func asRepositoryError(err error, target **RepositoryError) bool {
	repoErr, ok := err.(*RepositoryError)
	if ok {
		*target = repoErr
	}
	return ok
}

func createGitRepo(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	updateGitRepo(t, dir, files)
	return dir
}

func updateGitRepo(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "update")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func assertJSONFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("%s is not json: %v", path, err)
	}
}

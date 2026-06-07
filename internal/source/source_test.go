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

func TestPrepareWorktreeAndAttachPackagePlugin(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	openwrtRepo := createGitRepo(t, "openwrt", map[string]string{
		"README.md":                    "openwrt\n",
		"feeds.conf.default":           "src-git base https://example.invalid/base.git\n",
		"target/linux/x86/64/Makefile": "subtarget\n",
		"target/linux/x86/image/64.mk": "define Device/generic\nendef\nTARGET_DEVICES += generic\n",
	})
	feedRepo := createGitRepo(t, "packages", map[string]string{"README.md": "feed\n"})
	pluginRepo := createGitRepo(t, "plugin", map[string]string{"luci-app-demo/Makefile": "include $(TOPDIR)/rules.mk\n"})
	cfg := sourceTestConfig(openwrtRepo, feedRepo, pluginRepo)
	plans, err := config.UpdateSourceSetPlans(cfg, "x86-64")
	if err != nil {
		t.Fatal(err)
	}
	manager := Manager{Store: store}
	runID := "20260607T010203Z-abc123"
	runDir := filepath.Join(root, "workspaces", "auto-openwrt", "runs", "x86-64", runID)
	if _, err := manager.Update(context.Background(), UpdateInput{
		WorkspaceID: "auto-openwrt",
		BuildID:     stringPtr("x86-64"),
		RunID:       runID,
		RunDir:      runDir,
		Plans:       plans,
	}); err != nil {
		t.Fatal(err)
	}
	resolved, err := config.Resolve(config.ResolveInput{
		Config:      cfg,
		ProjectRoot: root,
		BuildID:     "x86-64",
		RunID:       runID,
		Env:         config.ResolveEnv{GOOS: "linux", CaseSensitive: true, CPUCount: 4},
	})
	if err != nil {
		t.Fatal(err)
	}

	prepared, err := manager.PrepareWorktree(context.Background(), PrepareWorktreeInput{Resolved: resolved, RunDir: runDir})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONFile(t, prepared.ManifestPath)
	if prepared.Manifest.PhysicalSourcePath == "" {
		t.Fatalf("physical source path is empty")
	}
	if _, err := os.Stat(filepath.Join(prepared.Manifest.PhysicalSourcePath, "README.md")); err != nil {
		t.Fatal(err)
	}

	attached, err := manager.AttachPlugins(context.Background(), AttachInput{
		Resolved:     resolved,
		RunDir:       runDir,
		Manifest:     prepared.Manifest,
		ManifestPath: prepared.ManifestPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONFile(t, attached.SummaryPath)
	feedsConf := filepath.Join(prepared.Manifest.PhysicalSourcePath, "feeds.conf")
	data, err := os.ReadFile(feedsConf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "src-link packages /auto-openwrt/sources/"+resolved.SourceSetID+"/feeds/packages") {
		t.Fatalf("feeds.conf missing src-link:\n%s", string(data))
	}
	assertFileContent(t, filepath.Join(prepared.Manifest.PhysicalSourcePath, "package", "auto-openwrt", "demo", "Makefile"), "include $(TOPDIR)/rules.mk\n")
	if len(attached.Summary.Feeds) != 1 || len(attached.Summary.Plugins) != 1 {
		t.Fatalf("attach summary = %#v", attached.Summary)
	}
}

func TestAttachPluginsInDockerVolumeUsesHelperAndWritesSummary(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	sourceSetID := "src-abc123abc123"
	feedPath := filepath.Join(root, "sources", "source-sets", sourceSetID, "feeds", "packages")
	packagePath := filepath.Join(root, "sources", "source-sets", sourceSetID, "plugins", "demo", "luci-app-demo")
	patchPath := filepath.Join(root, "sources", "source-sets", sourceSetID, "plugins", "fixes")
	for _, path := range []string{feedPath, packagePath, patchPath} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(packagePath, "Makefile"), []byte("package\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(patchPath, "001.patch"), []byte("patch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved := &config.ResolvedConfig{
		WorkspaceID: "auto-openwrt",
		SourceSetID: sourceSetID,
		BuildID:     "x86-64",
		RunID:       "20260607T010203Z-abc123",
		Docker:      config.ResolvedDocker{Image: "example/build-env:test", Platform: "linux/amd64"},
		Build: config.ResolvedBuild{
			ID:      "x86-64",
			Feeds:   []string{"packages"},
			Plugins: []string{"demo", "fixes"},
		},
		Feeds: []config.ResolvedFeed{{Name: "packages", Enabled: true}},
		Plugins: []config.ResolvedPlugin{
			{Name: "demo", Type: "package", Path: "luci-app-demo", Enabled: true, Risk: "luci-app"},
			{Name: "fixes", Type: "patch", Enabled: true, Risk: "patch"},
		},
	}
	runner := &recordingDockerRunner{stdout: "src-git base https://example.invalid/base.git\n"}
	manager := Manager{Store: store, Docker: runner, Now: func() time.Time { return time.Date(2026, 6, 7, 1, 2, 3, 0, time.UTC) }}

	result, err := manager.AttachPlugins(context.Background(), AttachInput{
		Resolved: resolved,
		RunDir:   filepath.Join(root, "workspaces", "auto-openwrt", "runs", "x86-64", resolved.RunID),
		Manifest: WorktreeManifest{
			StorageDriver:    "docker-volume",
			DockerVolumeName: "auto-openwrt-volume",
			SourceSetSnapshot: SourceSetSnapshot{
				Feeds:   []RepositorySnapshot{{Name: "packages", Commit: "feed123"}},
				Plugins: []PluginSnapshot{{Name: "demo", Commit: "demo123"}, {Name: "fixes", Commit: "fix123"}},
			},
		},
	})

	if err != nil {
		t.Fatal(err)
	}
	assertJSONFile(t, result.SummaryPath)
	if len(result.Summary.Feeds) != 1 || len(result.Summary.Plugins) != 2 {
		t.Fatalf("summary = %#v", result.Summary)
	}
	if result.Summary.Plugins[0].TargetPath == "" || result.Summary.Plugins[1].TargetPath == "" {
		t.Fatalf("plugin target paths missing: %#v", result.Summary.Plugins)
	}
	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{"--platform linux/amd64", "cp -a /auto-openwrt/plugin/.", "git apply --check", "cp /auto-openwrt/attach/feeds.conf /openwrt/feeds.conf"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("docker helper commands missing %q:\n%s", want, joined)
		}
	}
}

func TestPrepareDockerVolumeWorktreePassesPlatformToHelpers(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	sourceSetID := "src-abc123abc123"
	cachePath := filepath.Join(root, "sources", "source-sets", sourceSetID, "openwrt")
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatal(err)
	}
	snapshot := SourceSetSnapshot{
		SchemaVersion: SchemaVersion,
		SourceSetID:   sourceSetID,
		OpenWrt: RepositorySnapshot{
			Name:      "openwrt",
			Commit:    "abc123",
			CachePath: cachePath,
		},
	}
	if err := writeJSON(filepath.Join(root, "sources", "source-sets", sourceSetID, "source-set.json"), snapshot); err != nil {
		t.Fatal(err)
	}
	patchDir := filepath.Join(root, "workspaces", "auto-openwrt", "patches", "adopted", "x86-64")
	if err := os.MkdirAll(patchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(patchDir, "patch-1.patch"), []byte("diff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved := &config.ResolvedConfig{
		RunID:       "20260607T010203Z-abc123",
		WorkspaceID: "auto-openwrt",
		SourceSetID: sourceSetID,
		BuildID:     "x86-64",
		Workspace: config.ResolvedWorkspace{
			ID:                "auto-openwrt",
			Name:              "auto-openwrt",
			WorktreeStorage:   "docker-volume",
			LogicalWorktreeID: "workspaces/auto-openwrt/worktrees/x86-64/20260607T010203Z-abc123/",
		},
		Docker:          config.ResolvedDocker{Image: "example/build-env:test", Platform: "linux/amd64"},
		AdoptedPatchIDs: []string{"patch-1"},
	}
	runner := &recordingDockerRunner{}
	manager := Manager{Store: store, Docker: runner, Now: func() time.Time { return time.Date(2026, 6, 7, 1, 2, 3, 0, time.UTC) }}

	_, err = manager.PrepareWorktree(context.Background(), PrepareWorktreeInput{
		Resolved: resolved,
		RunDir:   filepath.Join(root, "workspaces", "auto-openwrt", "runs", "x86-64", resolved.RunID),
	})
	if err != nil {
		t.Fatal(err)
	}

	platformRuns := 0
	for _, command := range runner.commands {
		if strings.Contains(command, "run --rm") {
			if !strings.Contains(command, "--platform linux/amd64") {
				t.Fatalf("docker helper missing platform:\n%s", command)
			}
			platformRuns++
		}
	}
	if platformRuns != 2 {
		t.Fatalf("platform helper runs = %d, want 2; commands:\n%s", platformRuns, strings.Join(runner.commands, "\n"))
	}
}

type recordingDockerRunner struct {
	commands []string
	stdout   string
	err      error
}

func (r *recordingDockerRunner) Run(ctx context.Context, args ...string) GitResult {
	r.commands = append(r.commands, strings.Join(args, " "))
	if r.err != nil {
		return GitResult{ExitCode: 1, Stderr: "failed", Err: r.err}
	}
	if strings.Contains(strings.Join(args, " "), "cat /openwrt/feeds.conf.default") {
		return GitResult{Stdout: r.stdout}
	}
	return GitResult{}
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

func sourceTestConfig(openwrtRepo, feedRepo, pluginRepo string) *config.UserConfig {
	return &config.UserConfig{
		Version: 1,
		Workspace: config.WorkspaceConfig{
			ID:              "auto-openwrt",
			Name:            "auto-openwrt",
			WorktreeStorage: "host-path",
		},
		OpenWrt: config.OpenWrtConfig{Repo: openwrtRepo, Branch: "main", Update: "latest"},
		Docker:  config.DockerConfig{Image: "example/build-env:test", Platform: "auto"},
		Builds: []config.BuildConfig{{
			ID:      "x86-64",
			OpenWrt: config.BuildOpenWrtConfig{Target: "x86", Subtarget: "64", Profile: "generic"},
			Feeds:   []string{"packages"},
			Plugins: []string{"demo"},
			Config:  config.BuildOptions{Fragments: []string{}, Packages: []string{}, Jobs: "auto"},
		}},
		Feeds: []config.FeedConfig{{
			Name:    "packages",
			Repo:    feedRepo,
			Branch:  "main",
			Path:    "feeds/packages",
			Enabled: boolPtr(true),
		}},
		Plugins: []config.PluginConfig{{
			Name:    "demo",
			Type:    "package",
			Repo:    pluginRepo,
			Branch:  "main",
			Path:    "luci-app-demo",
			Enabled: boolPtr(true),
			Risk:    "luci-app",
		}},
		Health:    config.HealthConfig{MinDiskGB: intPtr(1)},
		AIRepair:  config.AIRepairConfig{Enabled: boolPtr(false), Timeout: "30m", MaxRetries: intPtr(5), Adoption: "auto"},
		Artifacts: config.ArtifactsConfig{Retention: "keep-all"},
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s content = %q, want %q", path, string(data), want)
	}
}

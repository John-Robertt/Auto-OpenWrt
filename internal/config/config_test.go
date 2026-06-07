package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

const testRunID = "20260606T120000Z-abc123"

func TestSampleConfigResolvesDefaults(t *testing.T) {
	cfg := mustParseConfig(t, SampleYAML)

	resolved, err := Resolve(ResolveInput{
		Config:      cfg,
		ProjectRoot: t.TempDir(),
		BuildID:     "x86-64",
		RunID:       testRunID,
		Env: ResolveEnv{
			GOOS:          "linux",
			CaseSensitive: true,
			CPUCount:      12,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resolved.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", resolved.SchemaVersion)
	}
	if resolved.WorkspaceID != "auto-openwrt" {
		t.Fatalf("workspace_id = %s, want auto-openwrt", resolved.WorkspaceID)
	}
	if resolved.BuildID != "x86-64" {
		t.Fatalf("build_id = %s, want x86-64", resolved.BuildID)
	}
	if !strings.HasPrefix(resolved.SourceSetID, "src-") || len(resolved.SourceSetID) != len("src-123456789abc") {
		t.Fatalf("source_set_id = %s", resolved.SourceSetID)
	}
	if resolved.Workspace.WorktreeStorage != "host-path" {
		t.Fatalf("worktree_storage = %s, want host-path", resolved.Workspace.WorktreeStorage)
	}
	if resolved.Workspace.LogicalWorktreeID != "workspaces/auto-openwrt/worktrees/x86-64/"+testRunID+"/" {
		t.Fatalf("logical_worktree_id = %s", resolved.Workspace.LogicalWorktreeID)
	}
	if resolved.Docker.Image != "ghcr.io/auto-openwrt/build-env:openwrt-24.10" {
		t.Fatalf("docker.image = %s", resolved.Docker.Image)
	}
	if resolved.Docker.Platform != "auto" {
		t.Fatalf("docker.platform = %s, want auto", resolved.Docker.Platform)
	}
	if resolved.AIRepair.Adoption != "auto" {
		t.Fatalf("ai_repair.adoption = %s, want auto", resolved.AIRepair.Adoption)
	}
	if resolved.AIRepair.MaxRetries != 5 {
		t.Fatalf("ai_repair.max_retries = %d, want 5", resolved.AIRepair.MaxRetries)
	}
	if resolved.Build.Jobs != 12 {
		t.Fatalf("build.jobs = %d, want 12", resolved.Build.Jobs)
	}
	if len(resolved.AdoptedPatchIDs) != 0 {
		t.Fatalf("adopted_patch_ids = %v, want empty", resolved.AdoptedPatchIDs)
	}
}

func TestLoadUserConfigDerivesWorkspaceIDFromFilename(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "configs", "router-a.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := strings.Replace(SampleYAML, "  id: auto-openwrt\n", "", 1)
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUserConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workspace.ID != "router-a" {
		t.Fatalf("workspace.id = %s, want router-a", cfg.Workspace.ID)
	}
}

func TestLoadUserConfigRejectsInvalidDerivedWorkspaceID(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "configs", "-bad.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := strings.Replace(SampleYAML, "  id: auto-openwrt\n", "", 1)
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadUserConfig(path)
	expectConfigError(t, err, "workspace.id")
}

func TestResolveAutoStorageUsesDockerVolumeWhenProjectRootIsNotCaseSensitive(t *testing.T) {
	cfg := mustParseConfig(t, SampleYAML)

	resolved, err := Resolve(ResolveInput{
		Config:      cfg,
		ProjectRoot: t.TempDir(),
		BuildID:     "x86-64",
		RunID:       testRunID,
		Env:         ResolveEnv{GOOS: "linux", CaseSensitive: false, CPUCount: 4},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resolved.Workspace.WorktreeStorage != "docker-volume" {
		t.Fatalf("worktree_storage = %s, want docker-volume", resolved.Workspace.WorktreeStorage)
	}
}

func TestWriteResolvedConfigIsAtomicAndSingleWrite(t *testing.T) {
	cfg := mustParseConfig(t, SampleYAML)
	store, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve(ResolveInput{
		Config:      cfg,
		ProjectRoot: store.Root,
		BuildID:     "x86-64",
		RunID:       testRunID,
		Env:         ResolveEnv{GOOS: "linux", CaseSensitive: true, CPUCount: 4},
	})
	if err != nil {
		t.Fatal(err)
	}

	path, err := WriteResolvedConfig(store, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(path), "workspaces/auto-openwrt/config/resolved/x86-64/"+testRunID+".yaml") {
		t.Fatalf("resolved path = %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"schema_version: 1", "workspace_id: auto-openwrt", "build_id: x86-64", "source_set_id: src-", "worktree_storage: host-path", "platform: auto", "adoption: auto", "adopted_patch_ids: []"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("resolved config missing %q:\n%s", want, string(data))
		}
	}

	_, err = WriteResolvedConfig(store, resolved)
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("second write error = %T %[1]v, want ConfigError", err)
	}
	if cfgErr.Code != "RESOLVED_CONFIG_EXISTS" {
		t.Fatalf("code = %s, want RESOLVED_CONFIG_EXISTS", cfgErr.Code)
	}
}

func TestConfigValidationErrorsIncludePathAndSuggestion(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		path        string
		messagePart string
	}{
		{
			name:        "yaml syntax",
			yaml:        "version: [\n",
			path:        "$",
			messagePart: "YAML",
		},
		{
			name:        "top level not map",
			yaml:        "- item\n",
			path:        "$",
			messagePart: "顶层结构",
		},
		{
			name:        "unsupported version",
			yaml:        strings.Replace(SampleYAML, "version: 1", "version: 2", 1),
			path:        "version",
			messagePart: "version",
		},
		{
			name:        "missing docker image",
			yaml:        strings.Replace(SampleYAML, "image: ghcr.io/auto-openwrt/build-env:openwrt-24.10", "image: \"\"", 1),
			path:        "docker.image",
			messagePart: "docker.image",
		},
		{
			name: "duplicate build",
			yaml: strings.Replace(SampleYAML, "builds:\n", `builds:
  - id: x86-64
    openwrt:
      target: x86
      subtarget: "64"
      profile: generic
`, 1),
			path:        "builds[1].id",
			messagePart: "重复",
		},
		{
			name:        "disabled plugin reference",
			yaml:        strings.Replace(SampleYAML, "enabled: true\n    risk: luci-app", "enabled: false\n    risk: luci-app", 1),
			path:        "builds[0].plugins[0]",
			messagePart: "disabled",
		},
		{
			name:        "legacy profiles",
			yaml:        strings.Replace(SampleYAML, "builds:", "profiles:", 1),
			path:        "profiles",
			messagePart: "profiles",
		},
		{
			name:        "ai command required",
			yaml:        strings.Replace(SampleYAML, "enabled: false\n  command: \"\"", "enabled: true\n  command: \"\"", 1),
			path:        "ai_repair.command",
			messagePart: "command",
		},
		{
			name:        "health must be positive",
			yaml:        strings.Replace(SampleYAML, "min_disk_gb: 80", "min_disk_gb: 0", 1),
			path:        "health.min_disk_gb",
			messagePart: "大于 0",
		},
		{
			name:        "relative linux worktree root",
			yaml:        strings.Replace(SampleYAML, `linux_worktree_root: ""`, `linux_worktree_root: relative/path`, 1),
			path:        "workspace.linux_worktree_root",
			messagePart: "绝对路径",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseUserConfig([]byte(tt.yaml))
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("error = %T %[1]v, want ConfigError", err)
			}
			if cfgErr.Path != tt.path {
				t.Fatalf("path = %s, want %s", cfgErr.Path, tt.path)
			}
			if !strings.Contains(cfgErr.Message, tt.messagePart) {
				t.Fatalf("message = %q, want containing %q", cfgErr.Message, tt.messagePart)
			}
			if cfgErr.Suggestion == "" {
				t.Fatalf("suggestion is empty")
			}
		})
	}
}

func TestResolveRejectsMissingBuildAndBadRunID(t *testing.T) {
	cfg := mustParseConfig(t, SampleYAML)

	_, err := Resolve(ResolveInput{
		Config:      cfg,
		ProjectRoot: t.TempDir(),
		BuildID:     "missing",
		RunID:       testRunID,
		Env:         ResolveEnv{GOOS: "linux", CaseSensitive: true, CPUCount: 2},
	})
	expectConfigError(t, err, "build_id")

	_, err = Resolve(ResolveInput{
		Config:      cfg,
		ProjectRoot: t.TempDir(),
		BuildID:     "x86-64",
		RunID:       "bad",
		Env:         ResolveEnv{GOOS: "linux", CaseSensitive: true, CPUCount: 2},
	})
	expectConfigError(t, err, "run_id")
}

func TestUpdateSourceSetPlansDeduplicateAllBuilds(t *testing.T) {
	cfg := mustParseConfig(t, strings.Replace(SampleYAML, "builds:\n", `builds:
  - id: x86-alt
    openwrt:
      target: x86
      subtarget: "64"
      profile: generic
    feeds:
      - packages
      - luci
    plugins:
      - openclash
    config:
      fragments: []
      packages: []
      jobs: auto
`, 1))

	plans, err := UpdateSourceSetPlans(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want deduplicated 1", len(plans))
	}
	if plans[0].BuildIDs[0] != "x86-64" || plans[0].BuildIDs[1] != "x86-alt" {
		t.Fatalf("build ids = %v", plans[0].BuildIDs)
	}
	if len(plans[0].Feeds) != 2 || plans[0].Feeds[0].Name != "luci" || plans[0].Feeds[1].Name != "packages" {
		t.Fatalf("feeds not sorted by name: %#v", plans[0].Feeds)
	}
	if len(plans[0].Plugins) != 1 || plans[0].Plugins[0].Risk != "luci-app" {
		t.Fatalf("plugins = %#v", plans[0].Plugins)
	}
}

func TestUpdateSourceSetPlansCanSelectOneBuild(t *testing.T) {
	cfg := mustParseConfig(t, SampleYAML)

	plans, err := UpdateSourceSetPlans(cfg, "x86-64")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].BuildIDs) != 1 || plans[0].BuildIDs[0] != "x86-64" {
		t.Fatalf("plans = %#v", plans)
	}

	_, err = UpdateSourceSetPlans(cfg, "missing")
	expectConfigError(t, err, "build_id")
}

func mustParseConfig(t *testing.T, content string) *UserConfig {
	t.Helper()
	cfg, err := ParseUserConfig([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func expectConfigError(t *testing.T, err error, path string) {
	t.Helper()
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("error = %T %[1]v, want ConfigError", err)
	}
	if cfgErr.Path != path {
		t.Fatalf("path = %s, want %s", cfgErr.Path, path)
	}
	if cfgErr.Suggestion == "" {
		t.Fatalf("suggestion is empty")
	}
}

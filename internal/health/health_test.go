package health

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/John-Robertt/Auto-OpenWrt/internal/config"
	"github.com/John-Robertt/Auto-OpenWrt/internal/source"
)

func TestBuildContextFailsInvalidProfile(t *testing.T) {
	root := t.TempDir()
	worktree := filepath.Join(root, "workspaces", "auto-openwrt", "worktrees", "x86-64", "20260607T010203Z-abc123", "source")
	if err := os.MkdirAll(filepath.Join(worktree, "target", "linux", "x86", "64"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "workspaces", "auto-openwrt", "runs", "x86-64", "20260607T010203Z-abc123"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "workspaces", "auto-openwrt", "runs", "x86-64", "20260607T010203Z-abc123", "worktree-manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"schema_version":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := DefaultChecker{Docker: fakeHealthDockerRunner{}}.BuildContext(context.Background(), BuildContextInput{
		RunID:        "20260607T010203Z-abc123",
		ProjectRoot:  root,
		WorkspaceID:  "auto-openwrt",
		SourceSetID:  "src-abc123abc123",
		BuildID:      "x86-64",
		Resolved:     buildContextResolved(root, false),
		Manifest:     buildContextManifest(worktree),
		ManifestPath: manifestPath,
		AttachSummary: &source.AttachSummary{
			SchemaVersion: source.SchemaVersion,
			Feeds:         []source.AttachEntry{},
			Plugins:       []source.AttachEntry{},
		},
		Existing: &Report{SchemaVersion: SchemaVersion, Preflight: []Item{passItem("system.os", "ok", "ok", "ok")}, CanContinue: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CanContinue {
		t.Fatalf("CanContinue = true, want false")
	}
	if itemStatus(report.BuildContext, "openwrt.target") != Fail {
		t.Fatalf("openwrt.target status = %s, want fail; items=%#v", itemStatus(report.BuildContext, "openwrt.target"), report.BuildContext)
	}
}

func TestBuildContextBlocksAIOnDockerVolume(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"schema_version":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := buildContextManifest("")
	manifest.StorageDriver = "docker-volume"
	manifest.DockerVolumeName = "auto-openwrt-volume"
	manifest.PhysicalSourcePath = ""

	report, err := DefaultChecker{}.BuildContext(context.Background(), BuildContextInput{
		RunID:        "20260607T010203Z-abc123",
		ProjectRoot:  root,
		WorkspaceID:  "auto-openwrt",
		SourceSetID:  "src-abc123abc123",
		BuildID:      "x86-64",
		Resolved:     buildContextResolved(root, true),
		Manifest:     manifest,
		ManifestPath: manifestPath,
		AttachSummary: &source.AttachSummary{
			SchemaVersion: source.SchemaVersion,
			Feeds:         []source.AttachEntry{},
			Plugins:       []source.AttachEntry{},
		},
		Existing: &Report{SchemaVersion: SchemaVersion, Preflight: []Item{passItem("system.os", "ok", "ok", "ok")}, CanContinue: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CanContinue {
		t.Fatalf("CanContinue = true, want false")
	}
	if itemStatus(report.BuildContext, "ai.worktree_access") != Fail {
		t.Fatalf("ai.worktree_access status = %s, want fail; items=%#v", itemStatus(report.BuildContext, "ai.worktree_access"), report.BuildContext)
	}
}

func TestBuildContextChecksDockerVolumeTargetWithHelper(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"schema_version":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := buildContextManifest("")
	manifest.StorageDriver = "docker-volume"
	manifest.DockerVolumeName = "auto-openwrt-volume"
	manifest.PhysicalSourcePath = ""

	report, err := DefaultChecker{Docker: fakeHealthDockerRunner{}}.BuildContext(context.Background(), BuildContextInput{
		RunID:        "20260607T010203Z-abc123",
		ProjectRoot:  root,
		WorkspaceID:  "auto-openwrt",
		SourceSetID:  "src-abc123abc123",
		BuildID:      "x86-64",
		Resolved:     buildContextResolved(root, false),
		Manifest:     manifest,
		ManifestPath: manifestPath,
		AttachSummary: &source.AttachSummary{
			SchemaVersion: source.SchemaVersion,
			Feeds:         []source.AttachEntry{},
			Plugins:       []source.AttachEntry{},
		},
		Existing: &Report{SchemaVersion: SchemaVersion, Preflight: []Item{passItem("system.os", "ok", "ok", "ok")}, CanContinue: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if itemStatus(report.BuildContext, "openwrt.target") != Pass {
		t.Fatalf("openwrt.target status = %s, want pass; items=%#v", itemStatus(report.BuildContext, "openwrt.target"), report.BuildContext)
	}
}

func TestBuildContextFailsDockerVolumeInvalidTarget(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"schema_version":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := buildContextManifest("")
	manifest.StorageDriver = "docker-volume"
	manifest.DockerVolumeName = "auto-openwrt-volume"
	manifest.PhysicalSourcePath = ""

	report, err := DefaultChecker{Docker: fakeHealthDockerRunner{err: errors.New("missing target")}}.BuildContext(context.Background(), BuildContextInput{
		RunID:        "20260607T010203Z-abc123",
		ProjectRoot:  root,
		WorkspaceID:  "auto-openwrt",
		SourceSetID:  "src-abc123abc123",
		BuildID:      "x86-64",
		Resolved:     buildContextResolved(root, false),
		Manifest:     manifest,
		ManifestPath: manifestPath,
		AttachSummary: &source.AttachSummary{
			SchemaVersion: source.SchemaVersion,
			Feeds:         []source.AttachEntry{},
			Plugins:       []source.AttachEntry{},
		},
		Existing: &Report{SchemaVersion: SchemaVersion, Preflight: []Item{passItem("system.os", "ok", "ok", "ok")}, CanContinue: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CanContinue {
		t.Fatalf("CanContinue = true, want false")
	}
	if itemStatus(report.BuildContext, "openwrt.target") != Fail {
		t.Fatalf("openwrt.target status = %s, want fail; items=%#v", itemStatus(report.BuildContext, "openwrt.target"), report.BuildContext)
	}
}

type fakeHealthDockerRunner struct {
	err error
}

func (r fakeHealthDockerRunner) Run(ctx context.Context, args ...string) source.GitResult {
	if r.err != nil {
		return source.GitResult{ExitCode: 1, Stderr: "missing target", Err: r.err}
	}
	if !strings.Contains(strings.Join(args, " "), "auto-openwrt-volume:/openwrt") {
		return source.GitResult{ExitCode: 1, Stderr: "missing volume", Err: errors.New("missing volume")}
	}
	return source.GitResult{}
}

func buildContextResolved(root string, aiEnabled bool) *config.ResolvedConfig {
	return &config.ResolvedConfig{
		RunID:       "20260607T010203Z-abc123",
		WorkspaceID: "auto-openwrt",
		SourceSetID: "src-abc123abc123",
		BuildID:     "x86-64",
		ProjectRoot: root,
		Workspace: config.ResolvedWorkspace{
			ID:                "auto-openwrt",
			Name:              "auto-openwrt",
			WorktreeStorage:   "host-path",
			LogicalWorktreeID: "workspaces/auto-openwrt/worktrees/x86-64/20260607T010203Z-abc123/",
		},
		Docker:  config.ResolvedDocker{Image: "example/build-env:test", Platform: "auto", ContainerWorktreePath: "/openwrt"},
		Build:   config.ResolvedBuild{ID: "x86-64", Target: "x86", Subtarget: "64", Profile: "generic"},
		Feeds:   []config.ResolvedFeed{},
		Plugins: []config.ResolvedPlugin{},
		AIRepair: config.ResolvedAIRepair{
			Enabled: aiEnabled,
			Command: "codex",
		},
	}
}

func buildContextManifest(worktree string) source.WorktreeManifest {
	return source.WorktreeManifest{
		SchemaVersion:      source.SchemaVersion,
		WorkspaceID:        "auto-openwrt",
		SourceSetID:        "src-abc123abc123",
		BuildID:            "x86-64",
		RunID:              "20260607T010203Z-abc123",
		LogicalWorktreeID:  "workspaces/auto-openwrt/worktrees/x86-64/20260607T010203Z-abc123/",
		StorageDriver:      "host-path",
		PhysicalSourcePath: worktree,
		ContainerPath:      "/openwrt",
		CaseSensitive:      true,
	}
}

func itemStatus(items []Item, id string) Status {
	for _, item := range items {
		if item.ID == id {
			return item.Status
		}
	}
	return ""
}

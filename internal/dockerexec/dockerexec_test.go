package dockerexec

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

type fakeRunner struct {
	args   []string
	result RunResult
}

func (f *fakeRunner) Run(ctx context.Context, args []string, logPath string) RunResult {
	f.args = append([]string{}, args...)
	if err := os.WriteFile(logPath, []byte("docker output\n"), 0o644); err != nil {
		return RunResult{ExitCode: -1, Err: err}
	}
	return f.result
}

func (f *fakeRunner) Output(ctx context.Context, args []string) string {
	return "test-version"
}

func TestBuildUsesAllowedMountsAndSkipsAutoPlatform(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{result: RunResult{}}

	result, err := DefaultExecutor{Runner: runner}.Build(context.Background(), dockerInput(root))

	if err != nil {
		t.Fatal(err)
	}
	if result.LogPath == "" || result.SummaryPath == "" {
		t.Fatalf("result paths missing: %#v", result)
	}
	joined := strings.Join(runner.args, " ")
	if strings.Contains(joined, "--platform") {
		t.Fatalf("auto platform should not pass --platform: %v", runner.args)
	}
	if strings.Contains(joined, root+":") {
		t.Fatalf("project root must not be mounted: %v", runner.args)
	}
	for _, want := range []string{"/openwrt", "/openwrt/dl", "/auto-openwrt/cache/build", "/auto-openwrt/artifacts"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("docker args missing %s: %v", want, runner.args)
		}
	}
}

func TestBuildPassesExplicitPlatform(t *testing.T) {
	root := t.TempDir()
	input := dockerInput(root)
	input.Resolved.Docker.Platform = "linux/amd64"
	runner := &fakeRunner{result: RunResult{}}

	_, err := DefaultExecutor{Runner: runner}.Build(context.Background(), input)

	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.args, " ")
	if !strings.Contains(joined, "--platform linux/amd64") {
		t.Fatalf("docker args missing platform: %v", runner.args)
	}
}

func TestBuildClassifiesStartupAndBuildFailures(t *testing.T) {
	root := t.TempDir()
	startup := &fakeRunner{result: RunResult{ExitCode: -1, Err: errors.New("exec failed")}}

	_, err := DefaultExecutor{Runner: startup}.Build(context.Background(), dockerInput(root))

	dockerErr, ok := AsError(err)
	if !ok || dockerErr.Kind != KindStartup {
		t.Fatalf("startup error = %#v", err)
	}

	build := &fakeRunner{result: RunResult{ExitCode: 2, Err: errors.New("make failed")}}
	_, err = DefaultExecutor{Runner: build}.Build(context.Background(), dockerInput(root))
	dockerErr, ok = AsError(err)
	if !ok || dockerErr.Kind != KindBuild {
		t.Fatalf("build error = %#v", err)
	}
}

func dockerInput(root string) Input {
	worktree := filepath.Join(root, "workspaces", "auto-openwrt", "worktrees", "x86-64", "run", "source")
	runDir := filepath.Join(root, "workspaces", "auto-openwrt", "runs", "x86-64", "run")
	return Input{
		ProjectRoot: root,
		Resolved: &config.ResolvedConfig{
			RunID:       "20260607T000000Z-abc123",
			WorkspaceID: "auto-openwrt",
			SourceSetID: "src-123456789abc",
			BuildID:     "x86-64",
			Docker: config.ResolvedDocker{
				Image:    "example/build-env:test",
				Platform: "auto",
			},
			Build: config.ResolvedBuild{Jobs: 2},
		},
		Manifest: source.WorktreeManifest{
			StorageDriver:      "host-path",
			PhysicalSourcePath: worktree,
		},
		AttachSummary:   source.AttachSummary{},
		DownloadCache:   filepath.Join(root, "cache", "downloads"),
		BuildCache:      filepath.Join(root, "cache", "build"),
		ArtifactStaging: filepath.Join(root, "workspaces", "auto-openwrt", "artifacts", ".staging", "x86-64", "run"),
		LogPath:         filepath.Join(runDir, "logs", "docker-build.log"),
		SummaryPath:     filepath.Join(runDir, "docker-env-summary.json"),
	}
}

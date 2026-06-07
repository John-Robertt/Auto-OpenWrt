package artifact

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

func TestArchiveSuccessFinalizesArtifactAndWritesSuccessLock(t *testing.T) {
	root := t.TempDir()
	input := successInput(t, root)

	result, err := DefaultRecorder{}.ArchiveSuccess(context.Background(), input)

	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{result.ArtifactIndexPath, result.SuccessLockPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(input.ArtifactStaging); !os.IsNotExist(err) {
		t.Fatalf("staging should be finalized, stat err=%v", err)
	}
	if len(result.FirmwarePaths) != 1 {
		t.Fatalf("firmware paths = %#v", result.FirmwarePaths)
	}
}

func TestArchiveSuccessLockFailureReturnsBlockedCondition(t *testing.T) {
	root := t.TempDir()
	input := successInput(t, root)
	lockParentAsFile := filepath.Join(root, "workspaces", "auto-openwrt", "locks", "x86-64")
	if err := os.MkdirAll(filepath.Dir(lockParentAsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockParentAsFile, []byte("not-dir\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	input.SuccessLockPath = filepath.Join(lockParentAsFile, "success-lock.json")

	_, err := DefaultRecorder{}.ArchiveSuccess(context.Background(), input)

	artifactErr, ok := AsError(err)
	if !ok || artifactErr.Code != "SUCCESS_LOCK_WRITE_ERROR" {
		t.Fatalf("error = %#v", err)
	}
	if _, err := os.Stat(input.ArtifactFinal); err != nil {
		t.Fatalf("artifact final should already be visible before lock failure: %v", err)
	}
}

func TestArchiveSuccessCollectsFirmwareFromDockerVolume(t *testing.T) {
	root := t.TempDir()
	input := successInput(t, root)
	input.Resolved.Docker = config.ResolvedDocker{Image: "example/build-env:test", Platform: "linux/amd64"}
	input.Manifest.StorageDriver = "docker-volume"
	input.Manifest.DockerVolumeName = "auto-openwrt-volume"
	input.Manifest.PhysicalSourcePath = ""

	result, err := DefaultRecorder{Docker: &fakeArtifactDockerRunner{}}.ArchiveSuccess(context.Background(), input)

	if err != nil {
		t.Fatal(err)
	}
	if len(result.FirmwarePaths) != 1 || !strings.Contains(result.FirmwarePaths[0], "openwrt-volume-test.img") {
		t.Fatalf("firmware paths = %#v", result.FirmwarePaths)
	}
	if _, err := os.Stat(result.SuccessLockPath); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveSuccessDockerVolumeCollectFailureIsStructured(t *testing.T) {
	root := t.TempDir()
	input := successInput(t, root)
	input.Resolved.Docker = config.ResolvedDocker{Image: "example/build-env:test"}
	input.Manifest.StorageDriver = "docker-volume"
	input.Manifest.DockerVolumeName = "auto-openwrt-volume"
	input.Manifest.PhysicalSourcePath = ""

	_, err := DefaultRecorder{Docker: &fakeArtifactDockerRunner{err: errors.New("copy failed")}}.ArchiveSuccess(context.Background(), input)

	artifactErr, ok := AsError(err)
	if !ok || artifactErr.Code != "FIRMWARE_COLLECT_ERROR" {
		t.Fatalf("error = %#v", err)
	}
}

func TestArchiveFailureRequiresD6ContextFiles(t *testing.T) {
	root := t.TempDir()
	success := successInput(t, root)
	input := failureInput(success)
	if err := os.Remove(input.HealthReportPath); err != nil {
		t.Fatal(err)
	}

	_, err := DefaultRecorder{}.ArchiveFailure(context.Background(), input)

	artifactErr, ok := AsError(err)
	if !ok || artifactErr.Code != "ARTIFACT_COPY_ERROR" {
		t.Fatalf("error = %#v", err)
	}
	indexPath := filepath.Join(root, "workspaces", "auto-openwrt", "diagnostics", "x86-64", "20260607T000000Z-abc123", "failure-index.json")
	if _, err := os.Stat(indexPath); !os.IsNotExist(err) {
		t.Fatalf("failure-index should not exist when required context is missing: %v", err)
	}
}

func TestArchiveFailureCopiesRequiredContextFiles(t *testing.T) {
	root := t.TempDir()
	input := failureInput(successInput(t, root))

	result, err := DefaultRecorder{}.ArchiveFailure(context.Background(), input)

	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"docker-build.log", "resolved-config.yaml", "health-report.json", "worktree-manifest.json", "docker-env-summary.json", "source-update-summary.json", "diagnostic-context.json", "last-summary.json", "failure-index.json"} {
		if _, err := os.Stat(filepath.Join(result.DiagnosticsDir, name)); err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
}

type fakeArtifactDockerRunner struct {
	err error
}

func (r *fakeArtifactDockerRunner) Run(ctx context.Context, args ...string) source.GitResult {
	if r.err != nil {
		return source.GitResult{ExitCode: 1, Stderr: "copy failed", Err: r.err}
	}
	staging := ""
	for _, arg := range args {
		if strings.HasSuffix(arg, ":/auto-openwrt/artifacts") {
			staging = strings.TrimSuffix(arg, ":/auto-openwrt/artifacts")
			break
		}
	}
	if staging == "" {
		return source.GitResult{ExitCode: 1, Stderr: "staging mount missing", Err: errors.New("staging mount missing")}
	}
	firmware := filepath.Join(staging, "firmware", "openwrt-volume-test.img")
	if err := os.MkdirAll(filepath.Dir(firmware), 0o755); err != nil {
		return source.GitResult{ExitCode: 1, Err: err}
	}
	if err := os.WriteFile(firmware, []byte("firmware\n"), 0o644); err != nil {
		return source.GitResult{ExitCode: 1, Err: err}
	}
	return source.GitResult{}
}

func successInput(t *testing.T, root string) SuccessInput {
	t.Helper()
	resolved := &config.ResolvedConfig{
		RunID:       "20260607T000000Z-abc123",
		WorkspaceID: "auto-openwrt",
		SourceSetID: "src-123456789abc",
		BuildID:     "x86-64",
		Build: config.ResolvedBuild{
			Target:    "x86",
			Subtarget: "64",
		},
	}
	worktree := filepath.Join(root, "worktree")
	firmware := filepath.Join(worktree, "bin", "targets", "x86", "64", "openwrt-test-squashfs.img")
	if err := os.MkdirAll(filepath.Dir(firmware), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firmware, []byte("firmware\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(root, "workspaces", "auto-openwrt", "runs", "x86-64", "20260607T000000Z-abc123")
	paths := map[string]string{
		"health":   filepath.Join(runDir, "health-report.json"),
		"resolved": filepath.Join(runDir, "resolved-config.yaml"),
		"source":   filepath.Join(runDir, "source-update-summary.json"),
		"manifest": filepath.Join(runDir, "worktree-manifest.json"),
		"log":      filepath.Join(runDir, "logs", "docker-build.log"),
		"docker":   filepath.Join(runDir, "docker-env-summary.json"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"schema_version":1}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return SuccessInput{
		ProjectRoot: root,
		Resolved:    resolved,
		Manifest: source.WorktreeManifest{
			PhysicalSourcePath: worktree,
			SourceSetSnapshot: source.SourceSetSnapshot{
				OpenWrt: source.RepositorySnapshot{Commit: "abc123"},
			},
		},
		ManifestPath:            paths["manifest"],
		HealthReportPath:        paths["health"],
		ResolvedConfigPath:      paths["resolved"],
		SourceUpdateSummaryPath: paths["source"],
		DockerLogPath:           paths["log"],
		DockerSummaryPath:       paths["docker"],
		RunDir:                  runDir,
		ArtifactStaging:         filepath.Join(root, "workspaces", "auto-openwrt", "artifacts", ".staging", "x86-64", "20260607T000000Z-abc123"),
		ArtifactFinal:           filepath.Join(root, "workspaces", "auto-openwrt", "artifacts", "x86-64", "20260607T000000Z-abc123"),
		SuccessLockPath:         filepath.Join(root, "workspaces", "auto-openwrt", "locks", "x86-64", "success-lock.json"),
	}
}

func failureInput(input SuccessInput) FailureInput {
	return FailureInput{
		ProjectRoot:             input.ProjectRoot,
		Resolved:                input.Resolved,
		Manifest:                input.Manifest,
		ManifestPath:            input.ManifestPath,
		HealthReportPath:        input.HealthReportPath,
		ResolvedConfigPath:      input.ResolvedConfigPath,
		SourceUpdateSummaryPath: input.SourceUpdateSummaryPath,
		DockerLogPath:           input.DockerLogPath,
		DockerSummaryPath:       input.DockerSummaryPath,
		RunDir:                  input.RunDir,
		FailureStage:            "docker.build",
		FailureTarget:           "x86/64",
		ErrorCode:               "OPENWRT_BUILD_FAILED",
		ErrorMessage:            "OpenWrt 构建命令失败",
	}
}

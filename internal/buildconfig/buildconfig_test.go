package buildconfig

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

func TestGenerateWritesTargetUserAndPackageFragments(t *testing.T) {
	root := t.TempDir()
	userFragment := filepath.Join(root, "fragments", "wifi.config")
	if err := os.MkdirAll(filepath.Dir(userFragment), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFragment, []byte("CONFIG_WIFI=y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "worktree")
	runDir := filepath.Join(root, "run")
	resolved := minimalResolved(root)
	resolved.Build.Fragments = []string{"fragments/wifi.config"}
	resolved.Build.Packages = []string{"luci", "-ppp"}

	result, err := DefaultGenerator{}.Generate(context.Background(), Input{
		ProjectRoot: root,
		Resolved:    resolved,
		Manifest:    source.WorktreeManifest{StorageDriver: "host-path", PhysicalSourcePath: worktree},
		RunDir:      runDir,
	})

	if err != nil {
		t.Fatal(err)
	}
	target := readFile(t, filepath.Join(result.FragmentDir, "00-target.config"))
	for _, want := range []string{"CONFIG_TARGET_x86=y", "CONFIG_TARGET_x86_64=y", "CONFIG_TARGET_x86_64_DEVICE_generic=y"} {
		if !strings.Contains(target, want) {
			t.Fatalf("target fragment missing %q:\n%s", want, target)
		}
	}
	user := readFile(t, filepath.Join(result.FragmentDir, "10-user-000.config"))
	if user != "CONFIG_WIFI=y\n" {
		t.Fatalf("user fragment = %q", user)
	}
	packages := readFile(t, filepath.Join(result.FragmentDir, "20-packages.config"))
	for _, want := range []string{"CONFIG_PACKAGE_luci=y", "# CONFIG_PACKAGE_ppp is not set"} {
		if !strings.Contains(packages, want) {
			t.Fatalf("package fragment missing %q:\n%s", want, packages)
		}
	}
	if _, err := os.Stat(result.SummaryPath); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateMissingUserFragmentReturnsStructuredError(t *testing.T) {
	root := t.TempDir()
	resolved := minimalResolved(root)
	resolved.Build.Fragments = []string{"missing.config"}

	_, err := DefaultGenerator{}.Generate(context.Background(), Input{
		ProjectRoot: root,
		Resolved:    resolved,
		Manifest:    source.WorktreeManifest{StorageDriver: "host-path", PhysicalSourcePath: filepath.Join(root, "worktree")},
		RunDir:      filepath.Join(root, "run"),
	})

	cfgErr, ok := AsError(err)
	if !ok || cfgErr.Code != "BUILD_CONFIG_FRAGMENT_MISSING" {
		t.Fatalf("error = %#v", err)
	}
}

func TestGenerateDockerVolumeCopiesFragmentsWithHelper(t *testing.T) {
	root := t.TempDir()
	resolved := minimalResolved(root)
	resolved.Docker = config.ResolvedDocker{Image: "example/build-env:test", Platform: "linux/amd64"}
	runner := &recordingDockerRunner{}

	result, err := DefaultGenerator{Docker: runner}.Generate(context.Background(), Input{
		ProjectRoot: root,
		Resolved:    resolved,
		Manifest:    source.WorktreeManifest{StorageDriver: "docker-volume", DockerVolumeName: "auto-openwrt-volume"},
		RunDir:      filepath.Join(root, "run"),
	})

	if err != nil {
		t.Fatal(err)
	}
	if result.FragmentDir != "/openwrt/.auto-openwrt/config-fragments" {
		t.Fatalf("fragment dir = %s", result.FragmentDir)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %#v", runner.commands)
	}
	command := runner.commands[0]
	for _, want := range []string{"--platform linux/amd64", "auto-openwrt-volume:/openwrt", "cp -a /auto-openwrt/config-fragments/."} {
		if !strings.Contains(command, want) {
			t.Fatalf("helper command missing %q:\n%s", want, command)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "run", "build-config-fragments", "00-target.config")); err != nil {
		t.Fatal(err)
	}
}

type recordingDockerRunner struct {
	commands []string
	err      error
}

func (r *recordingDockerRunner) Run(ctx context.Context, args ...string) source.GitResult {
	r.commands = append(r.commands, strings.Join(args, " "))
	if r.err != nil {
		return source.GitResult{ExitCode: 1, Stderr: "failed", Err: r.err}
	}
	return source.GitResult{}
}

func TestGenerateDockerVolumeHelperFailureIsStructured(t *testing.T) {
	root := t.TempDir()
	resolved := minimalResolved(root)
	resolved.Docker = config.ResolvedDocker{Image: "example/build-env:test"}

	_, err := DefaultGenerator{Docker: &recordingDockerRunner{err: errors.New("docker failed")}}.Generate(context.Background(), Input{
		ProjectRoot: root,
		Resolved:    resolved,
		Manifest:    source.WorktreeManifest{StorageDriver: "docker-volume", DockerVolumeName: "auto-openwrt-volume"},
		RunDir:      filepath.Join(root, "run"),
	})

	cfgErr, ok := AsError(err)
	if !ok || cfgErr.Code != "BUILD_CONFIG_WRITE_ERROR" {
		t.Fatalf("error = %#v", err)
	}
}

func minimalResolved(root string) *config.ResolvedConfig {
	return &config.ResolvedConfig{
		SchemaVersion: 1,
		RunID:         "20260607T000000Z-abc123",
		WorkspaceID:   "auto-openwrt",
		SourceSetID:   "src-123456789abc",
		BuildID:       "x86-64",
		ProjectRoot:   root,
		Build: config.ResolvedBuild{
			ID:        "x86-64",
			Target:    "x86",
			Subtarget: "64",
			Profile:   "generic",
			Jobs:      2,
		},
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

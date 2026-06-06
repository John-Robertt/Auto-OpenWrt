package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/John-Robertt/Auto-OpenWrt/internal/app"
	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

func TestRootHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run(nil, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{"Usage:", "Commands:", "init", "doctor", "build", "update", "logs", "--project <path>"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("root help missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestCommandHelp(t *testing.T) {
	for _, cmd := range commands {
		t.Run(cmd.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := Run([]string{cmd.name, "--help"}, &stdout, &stderr)

			if code != exitOK {
				t.Fatalf("exit code = %d, want %d", code, exitOK)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			for _, want := range []string{cmd.summary, "Usage:", cmd.usage} {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("%s help missing %q:\n%s", cmd.name, want, stdout.String())
				}
			}
		})
	}
}

func TestHelpCommandSupportsCommandArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run([]string{"help", "doctor"}, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "auto-openwrt doctor") {
		t.Fatalf("doctor help not printed:\n%s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestUnknownCommandReturnsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run([]string{"missing"}, &stdout, &stderr)

	if code != exitUsageError {
		t.Fatalf("exit code = %d, want %d", code, exitUsageError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{"unknown command", "suggestion:"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr.String())
		}
	}
}

func TestInitCommandCreatesWorkspaceWithJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := t.TempDir()

	code := Run([]string{"init", "--project", root, "--json"}, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr=%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	result := decodeResult(t, stdout.Bytes())
	if result.Status != "succeeded" {
		t.Fatalf("status = %s, want succeeded", result.Status)
	}
	if result.Command != "init" {
		t.Fatalf("command = %s, want init", result.Command)
	}
	if result.ProjectRoot != root {
		t.Fatalf("project_root = %s, want %s", result.ProjectRoot, root)
	}
	if result.WorkspaceID == nil || *result.WorkspaceID != "auto-openwrt" {
		t.Fatalf("workspace_id = %v, want auto-openwrt", result.WorkspaceID)
	}
	if result.BuildID != nil {
		t.Fatalf("build_id = %v, want nil", *result.BuildID)
	}
	if result.SourceSetID != nil {
		t.Fatalf("source_set_id = %v, want nil", *result.SourceSetID)
	}
	if result.RunID != nil {
		t.Fatalf("run_id = %v, want nil", *result.RunID)
	}
	if result.Paths["config"] != filepath.Join(root, "configs", "auto-openwrt.yaml") {
		t.Fatalf("config path = %s", result.Paths["config"])
	}
	for _, rel := range workspace.RequiredDirs {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", rel)
		}
	}
}

func TestInitExistingConfigReturnsConfigExists(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"init", "--project", root, "--json"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("first init exit code = %d, stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"init", "--project", root, "--json"}, &stdout, &stderr)

	if code != exitUsageError {
		t.Fatalf("exit code = %d, want %d", code, exitUsageError)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	result := decodeResult(t, stdout.Bytes())
	if result.Error == nil {
		t.Fatalf("error is nil")
	}
	if result.Error.Code != "CONFIG_EXISTS" {
		t.Fatalf("error code = %s, want CONFIG_EXISTS", result.Error.Code)
	}
}

func TestInitForceDoesNotDeleteExistingState(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"init", "--project", root}, &stdout, &stderr); code != exitOK {
		t.Fatalf("first init exit code = %d, stderr=%s", code, stderr.String())
	}
	keepPath := filepath.Join(root, "runs", "doctor", "keep.txt")
	if err := os.WriteFile(keepPath, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"init", "--project", root, "--force"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("force init exit code = %d, stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(keepPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep\n" {
		t.Fatalf("existing state content = %q", string(data))
	}
}

func TestInitUnknownFlagReturnsJSONErrorWhenJSONRequested(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run([]string{"init", "--json", "--bad"}, &stdout, &stderr)

	if code != exitUsageError {
		t.Fatalf("exit code = %d, want %d", code, exitUsageError)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	result := decodeResult(t, stdout.Bytes())
	if result.Error == nil || result.Error.Code != "INVALID_ARGUMENT" {
		t.Fatalf("result error = %#v", result.Error)
	}
}

func TestInitRejectsLegacyWorkspaceFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run([]string{"init", "--workspace", t.TempDir()}, &stdout, &stderr)

	if code != exitUsageError {
		t.Fatalf("exit code = %d, want %d", code, exitUsageError)
	}
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Fatalf("stderr does not reject legacy flag:\n%s", stderr.String())
	}
}

func TestBuildCommandIsNotImplementedInD3(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run([]string{"build"}, &stdout, &stderr)

	if code != exitUsageError {
		t.Fatalf("exit code = %d, want %d", code, exitUsageError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "not implemented in D2") {
		t.Fatalf("stderr does not explain boundary:\n%s", stderr.String())
	}
}

func TestUpdateCommandWithJSON(t *testing.T) {
	root := t.TempDir()
	repo := createCLIGitRepo(t)
	configPath := filepath.Join(root, "configs", "auto-openwrt.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(minimalUpdateConfig(repo)), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	code := Run([]string{"update", "--project", root, "--build", "x86-64", "--json"}, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr=%s stdout=%s", code, exitOK, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	result := decodeResult(t, stdout.Bytes())
	if result.Command != "update" || result.Status != "succeeded" {
		t.Fatalf("result = %#v", result)
	}
	if result.RunID == nil || result.SourceSetID == nil || result.BuildID == nil {
		t.Fatalf("run/source/build ids missing: %#v", result)
	}
	if result.Paths["source_update_summary"] == "" {
		t.Fatalf("source update summary path missing: %#v", result.Paths)
	}
	if _, err := os.Stat(result.Paths["source_update_summary"]); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorConfigErrorReturnsJSONWithoutRunRecord(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := t.TempDir()

	code := Run([]string{"doctor", "--project", root, "--json"}, &stdout, &stderr)

	if code != exitUsageError {
		t.Fatalf("exit code = %d, want %d", code, exitUsageError)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	result := decodeResult(t, stdout.Bytes())
	if result.Command != "doctor" || result.Error == nil || result.Error.Code != "CONFIG_READ_ERROR" {
		t.Fatalf("result = %#v", result)
	}
	if result.RunID != nil {
		t.Fatalf("run_id = %v, want nil", result.RunID)
	}
}

func TestLogsWithoutFinalRunReturnsJSONError(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"init", "--project", root, "--json"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("init exit code = %d, stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"logs", "--project", root, "--json"}, &stdout, &stderr)

	if code != exitUsageError {
		t.Fatalf("exit code = %d, want %d", code, exitUsageError)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	result := decodeResult(t, stdout.Bytes())
	if result.Command != "logs" || result.Error == nil || result.Error.Code != "RUN_NOT_FOUND" {
		t.Fatalf("result = %#v", result)
	}
}

func decodeResult(t *testing.T, data []byte) app.Result {
	t.Helper()
	var result app.Result
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("json decode failed: %v\n%s", err, string(data))
	}
	return result
}

func createCLIGitRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "openwrt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runCLIGit(t, dir, "init", "-b", "main")
	runCLIGit(t, dir, "config", "user.email", "test@example.com")
	runCLIGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("openwrt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLIGit(t, dir, "add", ".")
	runCLIGit(t, dir, "commit", "-m", "initial")
	return dir
}

func runCLIGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func minimalUpdateConfig(repo string) string {
	return `version: 1

workspace:
  id: auto-openwrt
  name: auto-openwrt
  worktree_storage: auto
  linux_worktree_root: ""

openwrt:
  repo: ` + repo + `
  branch: main
  update: latest

docker:
  image: example/build-env:test
  platform: auto

builds:
  - id: x86-64
    openwrt:
      target: x86
      subtarget: "64"
      profile: generic
    feeds: []
    plugins: []
    config:
      fragments: []
      packages: []
      jobs: auto

feeds: []
plugins: []

health:
  min_disk_gb: 1

ai_repair:
  enabled: false
  command: ""
  args: []
  timeout: 30m
  max_retries: 5
  adoption: auto

artifacts:
  retention: keep-all
`
}

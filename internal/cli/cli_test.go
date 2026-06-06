package cli

import (
	"bytes"
	"encoding/json"
	"os"
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

func TestBuildAndUpdateCommandsAreNotImplementedInD2(t *testing.T) {
	for _, command := range []string{"build", "update"} {
		t.Run(command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := Run([]string{command}, &stdout, &stderr)

			if code != exitUsageError {
				t.Fatalf("exit code = %d, want %d", code, exitUsageError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), "not implemented in D2") {
				t.Fatalf("stderr does not explain D2 boundary:\n%s", stderr.String())
			}
		})
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

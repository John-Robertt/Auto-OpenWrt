package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/John-Robertt/Auto-OpenWrt/internal/health"
	"github.com/John-Robertt/Auto-OpenWrt/internal/runrecord"
)

type fakeChecker struct {
	canContinue bool
}

func (f fakeChecker) Preflight(ctx context.Context, input health.PreflightInput) (*health.Report, error) {
	return &health.Report{
		SchemaVersion: health.SchemaVersion,
		RunID:         input.RunID,
		WorkspaceID:   input.WorkspaceID,
		SourceSetID:   input.SourceSetID,
		BuildID:       input.BuildID,
		ProjectRoot:   input.ProjectRoot,
		Preflight: []health.Item{{
			ID:         "system.os",
			Status:     health.Pass,
			Summary:    "ok",
			Detail:     "fake",
			Suggestion: "none",
		}},
		BuildContext:   []health.Item{},
		DockerImage:    "fake/image:test",
		DockerPlatform: "auto",
		CanContinue:    f.canContinue,
	}, nil
}

func TestDoctorSuccessCreatesRunAndHealthReport(t *testing.T) {
	root := initializedProject(t)

	result, code := Doctor(context.Background(), DoctorOptions{
		Project: root,
		Checker: fakeChecker{canContinue: true},
	})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d; error=%#v", code, ExitOK, result.Error)
	}
	if result.Status != "succeeded" {
		t.Fatalf("status = %s, want succeeded", result.Status)
	}
	if result.RunID == nil || *result.RunID == "" {
		t.Fatalf("run_id is empty")
	}
	if result.WorkspaceID == nil || *result.WorkspaceID != "auto-openwrt" {
		t.Fatalf("workspace_id = %v", result.WorkspaceID)
	}
	assertFileExists(t, result.Paths["run_record"])
	assertFileExists(t, result.Paths["health_report"])

	record, err := runrecord.Read(result.Paths["run_record"])
	if err != nil {
		t.Fatal(err)
	}
	if record.FinalStatus == nil || record.FinalStatus.Status != runrecord.FinalSucceeded {
		t.Fatalf("final_status = %#v", record.FinalStatus)
	}
	if record.Paths["health_report"] != result.Paths["health_report"] {
		t.Fatalf("run health path = %s, want %s", record.Paths["health_report"], result.Paths["health_report"])
	}
}

func TestDoctorHealthFailureReturnsBlockedAndExit3(t *testing.T) {
	root := initializedProject(t)

	result, code := Doctor(context.Background(), DoctorOptions{
		Project: root,
		Checker: fakeChecker{canContinue: false},
	})

	if code != ExitHealthBlocked {
		t.Fatalf("exit code = %d, want %d", code, ExitHealthBlocked)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %s, want blocked", result.Status)
	}
	if result.Error == nil || result.Error.Code != "HEALTH_CHECK_FAILED" {
		t.Fatalf("error = %#v", result.Error)
	}
	record, err := runrecord.Read(result.Paths["run_record"])
	if err != nil {
		t.Fatal(err)
	}
	if record.FinalStatus == nil || record.FinalStatus.Status != runrecord.FinalBlocked {
		t.Fatalf("final_status = %#v", record.FinalStatus)
	}
}

func TestDoctorConfigErrorDoesNotCreateRunRecord(t *testing.T) {
	root := t.TempDir()

	result, code := Doctor(context.Background(), DoctorOptions{
		Project: root,
		Checker: fakeChecker{canContinue: true},
	})

	if code != ExitUsageError {
		t.Fatalf("exit code = %d, want %d", code, ExitUsageError)
	}
	if result.Error == nil || result.Error.Code != "CONFIG_READ_ERROR" {
		t.Fatalf("error = %#v", result.Error)
	}
	matches, err := filepath.Glob(filepath.Join(root, "workspaces", "*", "runs", "doctor", "*", "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("run records created on config error: %v", matches)
	}
}

func TestDoctorMissingBuildReturnsUsageErrorWithoutRunRecord(t *testing.T) {
	root := initializedProject(t)

	result, code := Doctor(context.Background(), DoctorOptions{
		Project: root,
		BuildID: "missing",
		Checker: fakeChecker{canContinue: true},
	})

	if code != ExitUsageError {
		t.Fatalf("exit code = %d, want %d", code, ExitUsageError)
	}
	if result.Error == nil || result.Error.Code != "CONFIG_SCHEMA_ERROR" {
		t.Fatalf("error = %#v", result.Error)
	}
	matches, err := filepath.Glob(filepath.Join(root, "workspaces", "auto-openwrt", "runs", "doctor", "*", "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("run records created on missing build: %v", matches)
	}
}

func TestLogsReadsLatestFinalDoctorRun(t *testing.T) {
	root := initializedProject(t)
	doctorResult, code := Doctor(context.Background(), DoctorOptions{
		Project: root,
		Checker: fakeChecker{canContinue: true},
	})
	if code != ExitOK {
		t.Fatalf("doctor exit code = %d", code)
	}

	logsResult, code := Logs(LogsOptions{Project: root, Latest: true})

	if code != ExitOK {
		t.Fatalf("logs exit code = %d, error=%#v", code, logsResult.Error)
	}
	if logsResult.RunID == nil || doctorResult.RunID == nil || *logsResult.RunID != *doctorResult.RunID {
		t.Fatalf("logs run_id = %v, doctor run_id = %v", logsResult.RunID, doctorResult.RunID)
	}
	if logsResult.Paths["run_record"] != doctorResult.Paths["run_record"] {
		t.Fatalf("run_record = %s, want %s", logsResult.Paths["run_record"], doctorResult.Paths["run_record"])
	}
}

func initializedProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	result, code := Init(InitOptions{Project: root})
	if code != ExitOK {
		t.Fatalf("init exit code = %d, error=%#v", code, result.Error)
	}
	return root
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if path == "" {
		t.Fatal("path is empty")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("%s is not json: %v", path, err)
	}
}

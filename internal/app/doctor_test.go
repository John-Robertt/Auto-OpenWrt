package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/John-Robertt/Auto-OpenWrt/internal/artifact"
	"github.com/John-Robertt/Auto-OpenWrt/internal/buildconfig"
	"github.com/John-Robertt/Auto-OpenWrt/internal/dockerexec"
	"github.com/John-Robertt/Auto-OpenWrt/internal/health"
	"github.com/John-Robertt/Auto-OpenWrt/internal/runrecord"
	"github.com/John-Robertt/Auto-OpenWrt/internal/source"
	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

type fakeChecker struct {
	canContinue      bool
	buildCanContinue bool
}

type fakeSourceUpdater struct {
	input source.UpdateInput
	err   error
}

type fakeWorktreePreparer struct {
	input source.PrepareWorktreeInput
	err   error
}

type fakePluginAttacher struct {
	input source.AttachInput
	err   error
}

type fakeBuildConfigGenerator struct {
	input buildconfig.Input
	err   error
}

type fakeDockerExecutor struct {
	input dockerexec.Input
	err   error
}

type fakeArtifactRecorder struct {
	successInput artifact.SuccessInput
	failureInput artifact.FailureInput
	successErr   error
	failureErr   error
}

type mutatingChecker struct {
	fakeChecker
	replacement []byte
}

func (m mutatingChecker) Preflight(ctx context.Context, input health.PreflightInput) (*health.Report, error) {
	if err := os.WriteFile(input.ConfigPath, m.replacement, 0o644); err != nil {
		return nil, err
	}
	return m.fakeChecker.Preflight(ctx, input)
}

func (f *fakeSourceUpdater) Update(ctx context.Context, input source.UpdateInput) (*source.UpdateResult, error) {
	f.input = input
	if f.err != nil {
		return nil, f.err
	}
	summaryPath := filepath.Join(input.RunDir, "source-update-summary.json")
	if err := os.MkdirAll(input.RunDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(summaryPath, []byte(`{"schema_version":1}`+"\n"), 0o644); err != nil {
		return nil, err
	}
	snapshots := make([]source.SourceSetSnapshot, 0, len(input.Plans))
	sourceSetIDs := make([]string, 0, len(input.Plans))
	for _, plan := range input.Plans {
		snapshots = append(snapshots, source.SourceSetSnapshot{SourceSetID: plan.SourceSetID})
		sourceSetIDs = append(sourceSetIDs, plan.SourceSetID)
	}
	return &source.UpdateResult{SummaryPath: summaryPath, Snapshots: snapshots, SourceSetIDs: sourceSetIDs}, nil
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

func (f fakeChecker) BuildContext(ctx context.Context, input health.BuildContextInput) (*health.Report, error) {
	report := input.Existing
	if report == nil {
		report = &health.Report{SchemaVersion: health.SchemaVersion, RunID: input.RunID, ProjectRoot: input.ProjectRoot}
	}
	report.BuildContext = []health.Item{{
		ID:         "worktree.manifest",
		Status:     health.Pass,
		Summary:    "ok",
		Detail:     "fake",
		Suggestion: "none",
	}}
	report.CanContinue = f.buildCanContinue
	return report, nil
}

func (f *fakeWorktreePreparer) PrepareWorktree(ctx context.Context, input source.PrepareWorktreeInput) (*source.PrepareWorktreeResult, error) {
	f.input = input
	if f.err != nil {
		return nil, f.err
	}
	manifestPath := filepath.Join(input.RunDir, "worktree-manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"schema_version":1}`+"\n"), 0o644); err != nil {
		return nil, err
	}
	manifest := source.WorktreeManifest{
		SchemaVersion:      source.SchemaVersion,
		WorkspaceID:        input.Resolved.WorkspaceID,
		SourceSetID:        input.Resolved.SourceSetID,
		BuildID:            input.Resolved.BuildID,
		RunID:              input.Resolved.RunID,
		LogicalWorktreeID:  input.Resolved.Workspace.LogicalWorktreeID,
		StorageDriver:      input.Resolved.Workspace.WorktreeStorage,
		PhysicalSourcePath: filepath.Join(input.RunDir, "source"),
		ContainerPath:      "/openwrt",
		CaseSensitive:      true,
	}
	return &source.PrepareWorktreeResult{Manifest: manifest, ManifestPath: manifestPath}, nil
}

func (f *fakePluginAttacher) AttachPlugins(ctx context.Context, input source.AttachInput) (*source.AttachResult, error) {
	f.input = input
	if f.err != nil {
		return nil, f.err
	}
	summaryPath := filepath.Join(input.RunDir, "plugin-attach-summary.json")
	summary := source.AttachSummary{
		SchemaVersion: source.SchemaVersion,
		WorkspaceID:   input.Resolved.WorkspaceID,
		SourceSetID:   input.Resolved.SourceSetID,
		BuildID:       input.Resolved.BuildID,
		RunID:         input.Resolved.RunID,
		Feeds:         []source.AttachEntry{},
		Plugins:       []source.AttachEntry{},
	}
	if err := os.WriteFile(summaryPath, []byte(`{"schema_version":1}`+"\n"), 0o644); err != nil {
		return nil, err
	}
	return &source.AttachResult{SummaryPath: summaryPath, Summary: summary}, nil
}

func (f *fakeBuildConfigGenerator) Generate(ctx context.Context, input buildconfig.Input) (*buildconfig.Result, error) {
	f.input = input
	if f.err != nil {
		return nil, f.err
	}
	fragmentDir := filepath.Join(input.Manifest.PhysicalSourcePath, ".auto-openwrt", "config-fragments")
	if err := os.MkdirAll(fragmentDir, 0o755); err != nil {
		return nil, err
	}
	summaryPath := filepath.Join(input.RunDir, "build-config-summary.json")
	if err := os.WriteFile(summaryPath, []byte(`{"schema_version":1}`+"\n"), 0o644); err != nil {
		return nil, err
	}
	return &buildconfig.Result{FragmentDir: fragmentDir, SummaryPath: summaryPath}, nil
}

func (f *fakeDockerExecutor) Build(ctx context.Context, input dockerexec.Input) (*dockerexec.Result, error) {
	f.input = input
	if err := os.MkdirAll(filepath.Dir(input.LogPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(input.LogPath, []byte("docker log\n"), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(input.SummaryPath, []byte(`{"schema_version":1,"exit_code":0}`+"\n"), 0o644); err != nil {
		return nil, err
	}
	result := &dockerexec.Result{LogPath: input.LogPath, SummaryPath: input.SummaryPath}
	if f.err != nil {
		return result, f.err
	}
	return result, nil
}

func (f *fakeArtifactRecorder) ArchiveSuccess(ctx context.Context, input artifact.SuccessInput) (*artifact.SuccessResult, error) {
	f.successInput = input
	if f.successErr != nil {
		return nil, f.successErr
	}
	artifactIndex := filepath.Join(input.ArtifactFinal, "artifact-index.json")
	if err := os.MkdirAll(input.ArtifactFinal, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(artifactIndex, []byte(`{"schema_version":1}`+"\n"), 0o644); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(input.SuccessLockPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(input.SuccessLockPath, []byte(`{"schema_version":1}`+"\n"), 0o644); err != nil {
		return nil, err
	}
	return &artifact.SuccessResult{ArtifactIndexPath: artifactIndex, ArtifactDir: input.ArtifactFinal, SuccessLockPath: input.SuccessLockPath}, nil
}

func (f *fakeArtifactRecorder) ArchiveFailure(ctx context.Context, input artifact.FailureInput) (*artifact.FailureResult, error) {
	f.failureInput = input
	if f.failureErr != nil {
		return nil, f.failureErr
	}
	diagnosticsDir := filepath.Join(input.ProjectRoot, "workspaces", input.Resolved.WorkspaceID, "diagnostics", input.Resolved.BuildID, input.Resolved.RunID)
	if err := os.MkdirAll(diagnosticsDir, 0o755); err != nil {
		return nil, err
	}
	indexPath := filepath.Join(diagnosticsDir, "failure-index.json")
	if err := os.WriteFile(indexPath, []byte(`{"schema_version":1}`+"\n"), 0o644); err != nil {
		return nil, err
	}
	return &artifact.FailureResult{FailureIndexPath: indexPath, DiagnosticsDir: diagnosticsDir}, nil
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
	if record.Paths["user_config"] == "" {
		t.Fatalf("user config snapshot path missing: %#v", record.Paths)
	}
	configRead := stageByID(record, "config.read")
	if len(configRead.ResultPaths) != 1 || configRead.ResultPaths[0] != record.Paths["user_config"] {
		t.Fatalf("config.read result paths = %#v, user_config=%s", configRead.ResultPaths, record.Paths["user_config"])
	}
	if _, err := os.Stat(record.Paths["user_config"]); err != nil {
		t.Fatal(err)
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

func TestLogsBuildWithoutConfigSearchesAcrossWorkspaces(t *testing.T) {
	root := t.TempDir()
	store, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	runStore := runrecord.NewStore(store)
	buildID := "x86-64"
	olderWorkspace := "workspace-a"
	newerWorkspace := "workspace-b"
	createFinalRun(t, runStore, olderWorkspace, buildID, "20260607T010203Z-aaaaaa")
	newerPath := createFinalRun(t, runStore, newerWorkspace, buildID, "20260607T010204Z-bbbbbb")

	result, code := Logs(LogsOptions{Project: root, BuildID: buildID})

	if code != ExitOK {
		t.Fatalf("logs exit code = %d, error=%#v", code, result.Error)
	}
	if result.WorkspaceID == nil || *result.WorkspaceID != newerWorkspace {
		t.Fatalf("workspace_id = %v, want %s", result.WorkspaceID, newerWorkspace)
	}
	if result.Paths["run_record"] != newerPath {
		t.Fatalf("run_record = %s, want %s", result.Paths["run_record"], newerPath)
	}
}

func TestUpdateBuildSuccessCreatesRunAndSummary(t *testing.T) {
	root := initializedProject(t)
	updater := &fakeSourceUpdater{}

	result, code := Update(context.Background(), UpdateOptions{
		Project: root,
		BuildID: "x86-64",
		Updater: updater,
	})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d; error=%#v", code, ExitOK, result.Error)
	}
	if result.Status != "succeeded" {
		t.Fatalf("status = %s, want succeeded", result.Status)
	}
	if result.BuildID == nil || *result.BuildID != "x86-64" {
		t.Fatalf("build_id = %v", result.BuildID)
	}
	if result.SourceSetID == nil || !strings.HasPrefix(*result.SourceSetID, "src-") {
		t.Fatalf("source_set_id = %v", result.SourceSetID)
	}
	if len(updater.input.Plans) != 1 || updater.input.BuildID == nil || *updater.input.BuildID != "x86-64" {
		t.Fatalf("updater input = %#v", updater.input)
	}
	assertFileExists(t, result.Paths["run_record"])
	assertFileExists(t, result.Paths["source_update_summary"])
	record, err := runrecord.Read(result.Paths["run_record"])
	if err != nil {
		t.Fatal(err)
	}
	if record.FinalStatus == nil || record.FinalStatus.Status != runrecord.FinalSucceeded {
		t.Fatalf("final_status = %#v", record.FinalStatus)
	}
	if record.Paths["source_update_summary"] != result.Paths["source_update_summary"] {
		t.Fatalf("summary path not recorded: %#v", record.Paths)
	}
}

func TestUpdateWithoutBuildUsesSingleRunForAllSourceSets(t *testing.T) {
	root := initializedProject(t)
	configPath := filepath.Join(root, "configs", "auto-openwrt.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), "builds:\n", `builds:
  - id: no-extra
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
`, 1)
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	updater := &fakeSourceUpdater{}

	result, code := Update(context.Background(), UpdateOptions{Project: root, Updater: updater})

	if code != ExitOK {
		t.Fatalf("exit code = %d, error=%#v", code, result.Error)
	}
	if result.BuildID != nil {
		t.Fatalf("build_id = %v, want nil for multi-build update", result.BuildID)
	}
	if result.SourceSetID != nil {
		t.Fatalf("source_set_id = %v, want nil for multi-source-set update", result.SourceSetID)
	}
	if len(updater.input.Plans) != 2 {
		t.Fatalf("plans = %d, want 2", len(updater.input.Plans))
	}
}

func TestUpdateConfigErrorDoesNotCreateRunRecord(t *testing.T) {
	root := initializedProject(t)

	result, code := Update(context.Background(), UpdateOptions{Project: root, BuildID: "missing", Updater: &fakeSourceUpdater{}})

	if code != ExitUsageError {
		t.Fatalf("exit code = %d, want %d", code, ExitUsageError)
	}
	if result.Error == nil || result.Error.Code != "CONFIG_SCHEMA_ERROR" {
		t.Fatalf("error = %#v", result.Error)
	}
	matches, err := filepath.Glob(filepath.Join(root, "workspaces", "auto-openwrt", "runs", "update", "*", "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("run records created on config error: %v", matches)
	}
}

func TestUpdateSourceFailureReturnsExit4AndFailedRun(t *testing.T) {
	root := initializedProject(t)
	updater := &fakeSourceUpdater{err: &source.RepositoryError{Name: "openwrt", Repo: "file:///missing", ExitCode: 128, Err: errors.New("git failed")}}

	result, code := Update(context.Background(), UpdateOptions{Project: root, BuildID: "x86-64", Updater: updater})

	if code != ExitSourceError {
		t.Fatalf("exit code = %d, want %d", code, ExitSourceError)
	}
	if result.Status != "failed" {
		t.Fatalf("status = %s, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != "SOURCE_UPDATE_ERROR" || result.Error.Details["repository"] != "openwrt" {
		t.Fatalf("error = %#v", result.Error)
	}
	record, err := runrecord.Read(result.Paths["run_record"])
	if err != nil {
		t.Fatal(err)
	}
	if record.FinalStatus == nil || record.FinalStatus.Status != runrecord.FinalFailed {
		t.Fatalf("final_status = %#v", record.FinalStatus)
	}
}

func TestBuildPipelineArchivesSuccessfulDockerBuild(t *testing.T) {
	root := initializedProject(t)
	updater := &fakeSourceUpdater{}
	preparer := &fakeWorktreePreparer{}
	attacher := &fakePluginAttacher{}
	configGenerator := &fakeBuildConfigGenerator{}
	dockerExecutor := &fakeDockerExecutor{}
	recorder := &fakeArtifactRecorder{}

	result, code := Build(context.Background(), BuildOptions{
		Project:          root,
		BuildID:          "x86-64",
		Checker:          fakeChecker{canContinue: true, buildCanContinue: true},
		Updater:          updater,
		WorktreePreparer: preparer,
		PluginAttacher:   attacher,
		ConfigGenerator:  configGenerator,
		DockerExecutor:   dockerExecutor,
		ArtifactRecorder: recorder,
	})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d; error=%#v", code, ExitOK, result.Error)
	}
	if result.Status != "succeeded" {
		t.Fatalf("status = %s, want succeeded", result.Status)
	}
	if _, ok := result.Paths["artifact_staging"]; ok {
		t.Fatalf("build result should not expose artifact_staging: %#v", result.Paths)
	}
	for _, key := range []string{"run_record", "resolved_config", "health_report", "source_update_summary", "worktree_manifest", "plugin_attach_summary", "build_config_summary", "docker_build_log", "docker_env_summary", "artifact_index", "success_lock"} {
		if result.Paths[key] == "" {
			t.Fatalf("missing path %s in %#v", key, result.Paths)
		}
	}
	if len(updater.input.Plans) != 1 || updater.input.BuildID == nil || *updater.input.BuildID != "x86-64" {
		t.Fatalf("updater input = %#v", updater.input)
	}
	if preparer.input.Resolved == nil || preparer.input.Resolved.BuildID != "x86-64" {
		t.Fatalf("preparer input = %#v", preparer.input)
	}
	if attacher.input.Resolved == nil || attacher.input.Resolved.BuildID != "x86-64" {
		t.Fatalf("attacher input = %#v", attacher.input)
	}
	if configGenerator.input.Resolved == nil || configGenerator.input.Resolved.BuildID != "x86-64" {
		t.Fatalf("config generator input = %#v", configGenerator.input)
	}
	if dockerExecutor.input.Resolved == nil || dockerExecutor.input.Resolved.BuildID != "x86-64" {
		t.Fatalf("docker executor input = %#v", dockerExecutor.input)
	}
	if recorder.successInput.Resolved == nil || recorder.successInput.Resolved.BuildID != "x86-64" {
		t.Fatalf("artifact recorder input = %#v", recorder.successInput)
	}
	record, err := runrecord.Read(result.Paths["run_record"])
	if err != nil {
		t.Fatal(err)
	}
	if record.FinalStatus == nil || record.FinalStatus.Status != runrecord.FinalSucceeded {
		t.Fatalf("final_status = %#v", record.FinalStatus)
	}
	if record.Paths["user_config"] == "" {
		t.Fatalf("user config snapshot missing: %#v", record.Paths)
	}
	for _, stage := range []string{"config.read", "config.resolve", "health.preflight", "source.update", "worktree.prepare", "plugins.attach", "health.build_context", "build.config", "docker.build", "build.result", "artifact.archive"} {
		if !stageSucceeded(record, stage) {
			t.Fatalf("stage %s did not succeed: %#v", stage, record.Stages)
		}
	}
	logsResult, code := Logs(LogsOptions{Project: root, BuildID: "x86-64"})
	if code != ExitOK {
		t.Fatalf("logs exit code = %d, error=%#v", code, logsResult.Error)
	}
	if _, ok := logsResult.Paths["artifact_staging"]; ok {
		t.Fatalf("logs should not expose artifact_staging: %#v", logsResult.Paths)
	}
}

func TestBuildUsesBootstrapConfigSnapshotAfterOriginalFileChanges(t *testing.T) {
	root := initializedProject(t)
	originalConfigPath := filepath.Join(root, "configs", "auto-openwrt.yaml")
	original, err := os.ReadFile(originalConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	mutated := []byte(strings.Replace(string(original), "openwrt-24.10", "mutated-branch", 1))
	updater := &fakeSourceUpdater{}

	result, code := Build(context.Background(), BuildOptions{
		Project:          root,
		BuildID:          "x86-64",
		Checker:          mutatingChecker{fakeChecker: fakeChecker{canContinue: true, buildCanContinue: true}, replacement: mutated},
		Updater:          updater,
		WorktreePreparer: &fakeWorktreePreparer{},
		PluginAttacher:   &fakePluginAttacher{},
		ConfigGenerator:  &fakeBuildConfigGenerator{},
		DockerExecutor:   &fakeDockerExecutor{},
		ArtifactRecorder: &fakeArtifactRecorder{},
	})

	if code != ExitOK {
		t.Fatalf("exit code = %d, error=%#v", code, result.Error)
	}
	if len(updater.input.Plans) != 1 || updater.input.Plans[0].OpenWrt.Branch != "openwrt-24.10" {
		t.Fatalf("update plans used mutated config: %#v", updater.input.Plans)
	}
	record, err := runrecord.Read(result.Paths["run_record"])
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := os.ReadFile(record.Paths["user_config"])
	if err != nil {
		t.Fatal(err)
	}
	if string(snapshot) != string(original) {
		t.Fatalf("snapshot changed with original file:\n%s", string(snapshot))
	}
	current, err := os.ReadFile(originalConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(mutated) {
		t.Fatalf("test did not mutate original config")
	}
}

func TestBuildDockerFailureReturnsExit6AndArchivesFailure(t *testing.T) {
	root := initializedProject(t)
	dockerErr := &dockerexec.Error{
		Kind:       dockerexec.KindBuild,
		Code:       "OPENWRT_BUILD_FAILED",
		Message:    "OpenWrt 构建命令失败",
		Suggestion: "查看 docker-build.log",
		Details:    map[string]any{"exit_code": 2},
		Err:        errors.New("make failed"),
	}
	recorder := &fakeArtifactRecorder{}

	result, code := Build(context.Background(), BuildOptions{
		Project:          root,
		BuildID:          "x86-64",
		Checker:          fakeChecker{canContinue: true, buildCanContinue: true},
		Updater:          &fakeSourceUpdater{},
		WorktreePreparer: &fakeWorktreePreparer{},
		PluginAttacher:   &fakePluginAttacher{},
		ConfigGenerator:  &fakeBuildConfigGenerator{},
		DockerExecutor:   &fakeDockerExecutor{err: dockerErr},
		ArtifactRecorder: recorder,
	})

	if code != ExitOpenWrtError {
		t.Fatalf("exit code = %d, want %d", code, ExitOpenWrtError)
	}
	if result.Status != "failed" {
		t.Fatalf("status = %s, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != "OPENWRT_BUILD_FAILED" {
		t.Fatalf("error = %#v", result.Error)
	}
	if result.Paths["failure_index"] == "" {
		t.Fatalf("failure_index missing: %#v", result.Paths)
	}
	if recorder.failureInput.FailureStage != "docker.build" {
		t.Fatalf("failure input = %#v", recorder.failureInput)
	}
	record, err := runrecord.Read(result.Paths["run_record"])
	if err != nil {
		t.Fatal(err)
	}
	if record.FinalStatus == nil || record.FinalStatus.Status != runrecord.FinalFailed {
		t.Fatalf("final_status = %#v", record.FinalStatus)
	}
}

func TestBuildDockerFailureBlocksWhenFailureDiagnosisCannotArchive(t *testing.T) {
	root := initializedProject(t)
	dockerErr := &dockerexec.Error{
		Kind:       dockerexec.KindBuild,
		Code:       "OPENWRT_BUILD_FAILED",
		Message:    "OpenWrt 构建命令失败",
		Suggestion: "查看 docker-build.log",
		Details:    map[string]any{"exit_code": 2},
		Err:        errors.New("make failed"),
	}
	recorder := &fakeArtifactRecorder{failureErr: errors.New("archive failed")}

	result, code := Build(context.Background(), BuildOptions{
		Project:          root,
		BuildID:          "x86-64",
		Checker:          fakeChecker{canContinue: true, buildCanContinue: true},
		Updater:          &fakeSourceUpdater{},
		WorktreePreparer: &fakeWorktreePreparer{},
		PluginAttacher:   &fakePluginAttacher{},
		ConfigGenerator:  &fakeBuildConfigGenerator{},
		DockerExecutor:   &fakeDockerExecutor{err: dockerErr},
		ArtifactRecorder: recorder,
	})

	if code != ExitWorkspaceError {
		t.Fatalf("exit code = %d, want %d", code, ExitWorkspaceError)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %s, want blocked", result.Status)
	}
	record, err := runrecord.Read(result.Paths["run_record"])
	if err != nil {
		t.Fatal(err)
	}
	if record.FinalStatus == nil || record.FinalStatus.Status != runrecord.FinalBlocked || record.FinalStatus.Reason != "failure-diagnose-failed" {
		t.Fatalf("final_status = %#v", record.FinalStatus)
	}
}

func TestBuildPreflightFailureStopsBeforeSourceUpdate(t *testing.T) {
	root := initializedProject(t)
	updater := &fakeSourceUpdater{}

	result, code := Build(context.Background(), BuildOptions{
		Project: root,
		BuildID: "x86-64",
		Checker: fakeChecker{canContinue: false},
		Updater: updater,
	})

	if code != ExitHealthBlocked {
		t.Fatalf("exit code = %d, want %d", code, ExitHealthBlocked)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %s, want blocked", result.Status)
	}
	if len(updater.input.Plans) != 0 {
		t.Fatalf("source update should not run: %#v", updater.input)
	}
}

func stageSucceeded(record runrecord.RunRecord, id string) bool {
	return stageByID(record, id).Status == runrecord.StatusSucceeded
}

func stageByID(record runrecord.RunRecord, id string) runrecord.Stage {
	for _, stage := range record.Stages {
		if stage.ID == id {
			return stage
		}
	}
	return runrecord.Stage{}
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

func createFinalRun(t *testing.T, store runrecord.Store, workspaceID, buildID, runID string) string {
	t.Helper()
	sourceSetID := "src-123456789abc"
	record, runDir, err := store.Create(runrecord.CreateInput{
		Command:     "build",
		RunID:       runID,
		ProjectRoot: store.Workspace.Root,
		WorkspaceID: &workspaceID,
		SourceSetID: &sourceSetID,
		BuildID:     &buildID,
		RelDir:      runrecord.BuildRunRelDir(workspaceID, buildID, runID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.RunID != runID {
		t.Fatalf("run id = %s, want %s", record.RunID, runID)
	}
	if err := store.Complete(runDir, runrecord.FinalSucceeded, "", nil, fixedRunTime()); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(runDir, "run.json")
}

func fixedRunTime() time.Time {
	return time.Date(2026, 6, 7, 1, 2, 3, 0, time.UTC)
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

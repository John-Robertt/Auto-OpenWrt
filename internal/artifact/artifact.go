package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/John-Robertt/Auto-OpenWrt/internal/config"
	"github.com/John-Robertt/Auto-OpenWrt/internal/dockerexec"
	"github.com/John-Robertt/Auto-OpenWrt/internal/source"
	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

const SchemaVersion = 1

type Recorder interface {
	ArchiveSuccess(context.Context, SuccessInput) (*SuccessResult, error)
	ArchiveFailure(context.Context, FailureInput) (*FailureResult, error)
}

type SuccessInput struct {
	ProjectRoot             string
	Resolved                *config.ResolvedConfig
	Manifest                source.WorktreeManifest
	ManifestPath            string
	HealthReportPath        string
	ResolvedConfigPath      string
	SourceUpdateSummaryPath string
	DockerLogPath           string
	DockerSummaryPath       string
	RunDir                  string
	ArtifactStaging         string
	ArtifactFinal           string
	SuccessLockPath         string
}

type SuccessResult struct {
	ArtifactIndexPath string
	ArtifactDir       string
	SuccessLockPath   string
	FirmwarePaths     []string
}

type FailureInput struct {
	ProjectRoot             string
	Resolved                *config.ResolvedConfig
	Manifest                source.WorktreeManifest
	ManifestPath            string
	HealthReportPath        string
	ResolvedConfigPath      string
	SourceUpdateSummaryPath string
	DockerLogPath           string
	DockerSummaryPath       string
	RunDir                  string
	FailureStage            string
	FailureTarget           string
	ErrorCode               string
	ErrorMessage            string
}

type FailureResult struct {
	FailureIndexPath string
	DiagnosticsDir   string
	ContextPath      string
	LastSummaryPath  string
}

type ArtifactIndex struct {
	SchemaVersion         int      `json:"schema_version"`
	WorkspaceID           string   `json:"workspace_id"`
	SourceSetID           string   `json:"source_set_id"`
	BuildID               string   `json:"build_id"`
	RunID                 string   `json:"run_id"`
	ArtifactDir           string   `json:"artifact_dir"`
	FirmwarePaths         []string `json:"firmware_paths"`
	BuildLogPath          string   `json:"build_log_path"`
	ResolvedConfigPath    string   `json:"resolved_config_path"`
	HealthReportPath      string   `json:"health_report_path"`
	SuccessLockPath       string   `json:"success_lock_path"`
	SourceVersionPath     string   `json:"source_version_path"`
	DockerSummaryPath     string   `json:"docker_summary_path"`
	WorktreeManifestPath  string   `json:"worktree_manifest_path"`
	AdoptedPatchIDs       []string `json:"adopted_patch_ids"`
	AdoptedPatchIndexPath string   `json:"adopted_patch_index_path"`
	CreatedAt             string   `json:"created_at"`
}

type FailureIndex struct {
	SchemaVersion         int    `json:"schema_version"`
	WorkspaceID           string `json:"workspace_id"`
	SourceSetID           string `json:"source_set_id"`
	BuildID               string `json:"build_id"`
	RunID                 string `json:"run_id"`
	FailureStage          string `json:"failure_stage"`
	FailureTarget         string `json:"failure_target"`
	FailureLogPath        string `json:"failure_log_path"`
	DiagnosticContextPath string `json:"diagnostic_context_path"`
	HealthReportPath      string `json:"health_report_path"`
	ResolvedConfigPath    string `json:"resolved_config_path"`
	WorktreeManifestPath  string `json:"worktree_manifest_path"`
	CheckpointIndexPath   string `json:"checkpoint_index_path"`
	AIRepairHistoryPath   string `json:"ai_repair_history_path"`
	AIRepairDiffsPath     string `json:"ai_repair_diffs_path"`
	LastSummaryPath       string `json:"last_summary_path"`
	CreatedAt             string `json:"created_at"`
}

type SuccessLock struct {
	SchemaVersion      int                 `json:"schema_version"`
	SucceededAt        string              `json:"succeeded_at"`
	WorkspaceID        string              `json:"workspace_id"`
	SourceSetID        string              `json:"source_set_id"`
	BuildID            string              `json:"build_id"`
	RunID              string              `json:"run_id"`
	OpenWrtCommit      string              `json:"openwrt_commit"`
	FeedsCommit        map[string]string   `json:"feeds_commit"`
	PluginsCommit      map[string]string   `json:"plugins_commit"`
	AdoptedPatchIDs    []string            `json:"adopted_patch_ids"`
	ResolvedConfigPath string              `json:"resolved_config_path"`
	FirmwarePaths      []string            `json:"firmware_paths"`
	DockerSummaryPath  string              `json:"docker_summary_path"`
	DockerEnvironment  *dockerexec.Summary `json:"docker_environment,omitempty"`
}

type Error struct {
	Code       string
	Message    string
	Suggestion string
	Details    map[string]any
	Err        error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error {
	return e.Err
}

type DefaultRecorder struct {
	Docker source.DockerRunner
	Now    func() time.Time
}

func (r DefaultRecorder) ArchiveSuccess(ctx context.Context, input SuccessInput) (*SuccessResult, error) {
	_ = ctx
	if input.Resolved == nil {
		return nil, artifactError("ARTIFACT_INPUT_ERROR", "resolved config 为空", "先完成 config.resolve 阶段", nil, nil)
	}
	now := r.now()
	if err := os.RemoveAll(input.ArtifactStaging); err != nil {
		return nil, artifactError("ARTIFACT_STAGING_ERROR", "artifact staging 无法清理", "检查 staging 目录权限", map[string]any{"path": input.ArtifactStaging}, err)
	}
	if err := os.MkdirAll(input.ArtifactStaging, 0o755); err != nil {
		return nil, artifactError("ARTIFACT_STAGING_ERROR", "artifact staging 无法创建", "检查 workspace 权限和磁盘空间", map[string]any{"path": input.ArtifactStaging}, err)
	}
	copiedLog, err := copyRequired(input.DockerLogPath, filepath.Join(input.ArtifactStaging, "docker-build.log"))
	if err != nil {
		return nil, err
	}
	copiedResolved, err := copyRequired(input.ResolvedConfigPath, filepath.Join(input.ArtifactStaging, "resolved-config.yaml"))
	if err != nil {
		return nil, err
	}
	copiedHealth, err := copyRequired(input.HealthReportPath, filepath.Join(input.ArtifactStaging, "health-report.json"))
	if err != nil {
		return nil, err
	}
	copiedManifest, err := copyRequired(input.ManifestPath, filepath.Join(input.ArtifactStaging, "worktree-manifest.json"))
	if err != nil {
		return nil, err
	}
	copiedDockerSummary, err := copyRequired(input.DockerSummaryPath, filepath.Join(input.ArtifactStaging, "docker-env-summary.json"))
	if err != nil {
		return nil, err
	}
	copiedSource, err := copyRequired(input.SourceUpdateSummaryPath, filepath.Join(input.ArtifactStaging, "source-update-summary.json"))
	if err != nil {
		return nil, err
	}
	firmwarePaths, err := r.copyFirmware(ctx, input.Manifest, input.Resolved, input.ArtifactStaging)
	if err != nil {
		return nil, err
	}
	finalIndex := filepath.Join(input.ArtifactFinal, "artifact-index.json")
	index := ArtifactIndex{
		SchemaVersion:         SchemaVersion,
		WorkspaceID:           input.Resolved.WorkspaceID,
		SourceSetID:           input.Resolved.SourceSetID,
		BuildID:               input.Resolved.BuildID,
		RunID:                 input.Resolved.RunID,
		ArtifactDir:           input.ArtifactFinal,
		FirmwarePaths:         finalPaths(firmwarePaths, input.ArtifactStaging, input.ArtifactFinal),
		BuildLogPath:          finalPath(copiedLog, input.ArtifactStaging, input.ArtifactFinal),
		ResolvedConfigPath:    finalPath(copiedResolved, input.ArtifactStaging, input.ArtifactFinal),
		HealthReportPath:      finalPath(copiedHealth, input.ArtifactStaging, input.ArtifactFinal),
		SuccessLockPath:       input.SuccessLockPath,
		SourceVersionPath:     finalPath(copiedSource, input.ArtifactStaging, input.ArtifactFinal),
		DockerSummaryPath:     finalPath(copiedDockerSummary, input.ArtifactStaging, input.ArtifactFinal),
		WorktreeManifestPath:  finalPath(copiedManifest, input.ArtifactStaging, input.ArtifactFinal),
		AdoptedPatchIDs:       append([]string{}, input.Resolved.AdoptedPatchIDs...),
		AdoptedPatchIndexPath: filepath.Join(input.ProjectRoot, "workspaces", input.Resolved.WorkspaceID, "patches", "adopted", input.Resolved.BuildID, "index.json"),
		CreatedAt:             now.Format(time.RFC3339),
	}
	if err := writeJSON(filepath.Join(input.ArtifactStaging, "artifact-index.json"), index); err != nil {
		return nil, artifactError("ARTIFACT_INDEX_WRITE_ERROR", "artifact-index 无法写入", "检查 artifact staging 权限", map[string]any{"path": finalIndex}, err)
	}
	if err := os.MkdirAll(filepath.Dir(input.ArtifactFinal), 0o755); err != nil {
		return nil, artifactError("ARTIFACT_FINALIZE_ERROR", "artifact final 父目录无法创建", "检查 workspace 权限", map[string]any{"path": filepath.Dir(input.ArtifactFinal)}, err)
	}
	if _, err := os.Stat(input.ArtifactFinal); err == nil {
		return nil, artifactError("ARTIFACT_FINAL_EXISTS", "artifact final 目录已存在", "为新的 run 使用新的 run_id，或人工检查已有产物", map[string]any{"path": input.ArtifactFinal}, nil)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, artifactError("ARTIFACT_FINALIZE_ERROR", "artifact final 目录无法检查", "检查 workspace 权限", map[string]any{"path": input.ArtifactFinal}, err)
	}
	if err := os.Rename(input.ArtifactStaging, input.ArtifactFinal); err != nil {
		return nil, artifactError("ARTIFACT_FINALIZE_ERROR", "artifact staging 无法原子 finalize", "检查 staging 与 final 是否在同一文件系统并确认权限", map[string]any{"staging": input.ArtifactStaging, "final": input.ArtifactFinal}, err)
	}
	dockerSummary := readDockerSummary(input.DockerSummaryPath)
	lock := successLock(input, finalPaths(firmwarePaths, input.ArtifactStaging, input.ArtifactFinal), dockerSummary, now)
	if err := writeJSON(input.SuccessLockPath, lock); err != nil {
		return nil, artifactError("SUCCESS_LOCK_WRITE_ERROR", "success lock 无法写入", "检查 workspace locks 目录权限和磁盘空间", map[string]any{"path": input.SuccessLockPath}, err)
	}
	return &SuccessResult{ArtifactIndexPath: finalIndex, ArtifactDir: input.ArtifactFinal, SuccessLockPath: input.SuccessLockPath, FirmwarePaths: index.FirmwarePaths}, nil
}

func (r DefaultRecorder) ArchiveFailure(ctx context.Context, input FailureInput) (*FailureResult, error) {
	_ = ctx
	if input.Resolved == nil {
		return nil, artifactError("DIAGNOSTIC_INPUT_ERROR", "resolved config 为空", "先完成 config.resolve 阶段", nil, nil)
	}
	now := r.now()
	diagDir := filepath.Join(input.ProjectRoot, "workspaces", input.Resolved.WorkspaceID, "diagnostics", input.Resolved.BuildID, input.Resolved.RunID)
	if err := os.MkdirAll(diagDir, 0o755); err != nil {
		return nil, artifactError("DIAGNOSTIC_WRITE_ERROR", "diagnostics 目录无法创建", "检查 workspace 权限和磁盘空间", map[string]any{"path": diagDir}, err)
	}
	failureLog, err := copyRequired(input.DockerLogPath, filepath.Join(diagDir, "docker-build.log"))
	if err != nil {
		return nil, err
	}
	resolvedConfig, err := copyRequired(input.ResolvedConfigPath, filepath.Join(diagDir, "resolved-config.yaml"))
	if err != nil {
		return nil, err
	}
	healthReport, err := copyRequired(input.HealthReportPath, filepath.Join(diagDir, "health-report.json"))
	if err != nil {
		return nil, err
	}
	worktreeManifest, err := copyRequired(input.ManifestPath, filepath.Join(diagDir, "worktree-manifest.json"))
	if err != nil {
		return nil, err
	}
	dockerSummary, err := copyRequired(input.DockerSummaryPath, filepath.Join(diagDir, "docker-env-summary.json"))
	if err != nil {
		return nil, err
	}
	sourceSummary, err := copyRequired(input.SourceUpdateSummaryPath, filepath.Join(diagDir, "source-update-summary.json"))
	if err != nil {
		return nil, err
	}
	contextPath := filepath.Join(diagDir, "diagnostic-context.json")
	if err := writeJSON(contextPath, map[string]any{
		"schema_version":             SchemaVersion,
		"workspace_id":               input.Resolved.WorkspaceID,
		"source_set_id":              input.Resolved.SourceSetID,
		"build_id":                   input.Resolved.BuildID,
		"run_id":                     input.Resolved.RunID,
		"failure_stage":              input.FailureStage,
		"failure_target":             input.FailureTarget,
		"error_code":                 input.ErrorCode,
		"error_message":              input.ErrorMessage,
		"docker_log_path":            failureLog,
		"resolved_config_path":       resolvedConfig,
		"health_report_path":         healthReport,
		"worktree_manifest_path":     worktreeManifest,
		"docker_summary_path":        dockerSummary,
		"source_update_summary_path": sourceSummary,
		"plugin_risks":               pluginRisks(input.Resolved.Plugins),
		"created_at":                 now.Format(time.RFC3339),
	}); err != nil {
		return nil, artifactError("DIAGNOSTIC_WRITE_ERROR", "诊断上下文无法写入", "检查 diagnostics 目录权限", map[string]any{"path": contextPath}, err)
	}
	lastSummaryPath := filepath.Join(diagDir, "last-summary.json")
	if err := writeJSON(lastSummaryPath, map[string]any{
		"schema_version": SchemaVersion,
		"status":         "failed",
		"failure_stage":  input.FailureStage,
		"failure_target": input.FailureTarget,
		"message":        input.ErrorMessage,
		"created_at":     now.Format(time.RFC3339),
	}); err != nil {
		return nil, artifactError("DIAGNOSTIC_WRITE_ERROR", "最后现场摘要无法写入", "检查 diagnostics 目录权限", map[string]any{"path": lastSummaryPath}, err)
	}
	indexPath := filepath.Join(diagDir, "failure-index.json")
	index := FailureIndex{
		SchemaVersion:         SchemaVersion,
		WorkspaceID:           input.Resolved.WorkspaceID,
		SourceSetID:           input.Resolved.SourceSetID,
		BuildID:               input.Resolved.BuildID,
		RunID:                 input.Resolved.RunID,
		FailureStage:          input.FailureStage,
		FailureTarget:         input.FailureTarget,
		FailureLogPath:        failureLog,
		DiagnosticContextPath: contextPath,
		HealthReportPath:      healthReport,
		ResolvedConfigPath:    resolvedConfig,
		WorktreeManifestPath:  worktreeManifest,
		CheckpointIndexPath:   "",
		AIRepairHistoryPath:   "",
		AIRepairDiffsPath:     "",
		LastSummaryPath:       lastSummaryPath,
		CreatedAt:             now.Format(time.RFC3339),
	}
	if err := writeJSON(indexPath, index); err != nil {
		return nil, artifactError("FAILURE_INDEX_WRITE_ERROR", "failure-index 无法写入", "检查 diagnostics 目录权限", map[string]any{"path": indexPath}, err)
	}
	return &FailureResult{FailureIndexPath: indexPath, DiagnosticsDir: diagDir, ContextPath: contextPath, LastSummaryPath: lastSummaryPath}, nil
}

func (r DefaultRecorder) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r DefaultRecorder) copyFirmware(ctx context.Context, manifest source.WorktreeManifest, resolved *config.ResolvedConfig, staging string) ([]string, error) {
	if manifest.StorageDriver == "docker-volume" {
		return r.copyFirmwareFromDockerVolume(ctx, manifest, resolved, staging)
	}
	if manifest.PhysicalSourcePath == "" {
		return nil, artifactError("FIRMWARE_COLLECT_UNSUPPORTED", "工作树物理路径为空", "检查 worktree manifest，或重新运行 build 以准备当前 run 工作树", map[string]any{"storage_driver": manifest.StorageDriver}, nil)
	}
	sourceDir := filepath.Join(manifest.PhysicalSourcePath, "bin", "targets", resolved.Build.Target, resolved.Build.Subtarget)
	if _, err := os.Stat(sourceDir); err != nil {
		return nil, artifactError("FIRMWARE_NOT_FOUND", "OpenWrt 固件产物不存在", "查看 Docker 构建日志确认 make 是否生成 bin/targets 产物", map[string]any{"path": sourceDir}, err)
	}
	targetDir := filepath.Join(staging, "firmware")
	paths := []string{}
	err := filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".buildinfo") || strings.HasSuffix(name, ".manifest") || strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".sha256sums") {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(targetDir, rel)
		if err := copyFile(path, dst); err != nil {
			return err
		}
		paths = append(paths, dst)
		return nil
	})
	if err != nil {
		return nil, artifactError("FIRMWARE_COLLECT_ERROR", "固件产物收集失败", "检查 bin/targets 目录权限", map[string]any{"path": sourceDir}, err)
	}
	if len(paths) == 0 {
		return nil, artifactError("FIRMWARE_NOT_FOUND", "未找到可归档的固件文件", "查看 Docker 构建日志确认目标产物类型", map[string]any{"path": sourceDir}, nil)
	}
	sort.Strings(paths)
	return paths, nil
}

func (r DefaultRecorder) copyFirmwareFromDockerVolume(ctx context.Context, manifest source.WorktreeManifest, resolved *config.ResolvedConfig, staging string) ([]string, error) {
	if manifest.DockerVolumeName == "" {
		return nil, artifactError("FIRMWARE_COLLECT_UNSUPPORTED", "Docker volume 未记录", "重新准备运行工作树", map[string]any{"storage_driver": manifest.StorageDriver}, nil)
	}
	runner := r.Docker
	if runner == nil {
		runner = dockerExecRunner{}
	}
	sourceDir := "/openwrt/bin/targets/" + resolved.Build.Target + "/" + resolved.Build.Subtarget
	command := "test -d " + shellQuote(sourceDir) + " && mkdir -p /auto-openwrt/artifacts/firmware && cp -a " + shellQuote(sourceDir) + "/. /auto-openwrt/artifacts/firmware/"
	args := dockerHelperArgs(resolved, []string{"-v", manifest.DockerVolumeName + ":/openwrt", "-v", staging + ":/auto-openwrt/artifacts"}, command)
	if result := runner.Run(ctx, args...); !result.Success() {
		return nil, artifactError("FIRMWARE_COLLECT_ERROR", "docker-volume 固件产物收集失败", "查看 Docker 构建日志确认 make 是否生成 bin/targets 产物", map[string]any{"path": sourceDir, "stderr": strings.TrimSpace(result.Stderr)}, result.Err)
	}
	paths, err := firmwareFiles(filepath.Join(staging, "firmware"))
	if err != nil {
		return nil, artifactError("FIRMWARE_COLLECT_ERROR", "固件产物收集失败", "检查 artifact staging 目录权限", map[string]any{"path": filepath.Join(staging, "firmware")}, err)
	}
	if len(paths) == 0 {
		return nil, artifactError("FIRMWARE_NOT_FOUND", "未找到可归档的固件文件", "查看 Docker 构建日志确认目标产物类型", map[string]any{"path": sourceDir}, nil)
	}
	return paths, nil
}

func firmwareFiles(root string) ([]string, error) {
	paths := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".buildinfo") || strings.HasSuffix(name, ".manifest") || strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".sha256sums") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func successLock(input SuccessInput, firmware []string, dockerSummary *dockerexec.Summary, now time.Time) SuccessLock {
	feeds := map[string]string{}
	for _, feed := range input.Manifest.SourceSetSnapshot.Feeds {
		feeds[feed.Name] = feed.Commit
	}
	plugins := map[string]string{}
	for _, plugin := range input.Manifest.SourceSetSnapshot.Plugins {
		plugins[plugin.Name] = plugin.Commit
	}
	return SuccessLock{
		SchemaVersion:      SchemaVersion,
		SucceededAt:        now.Format(time.RFC3339),
		WorkspaceID:        input.Resolved.WorkspaceID,
		SourceSetID:        input.Resolved.SourceSetID,
		BuildID:            input.Resolved.BuildID,
		RunID:              input.Resolved.RunID,
		OpenWrtCommit:      input.Manifest.SourceSetSnapshot.OpenWrt.Commit,
		FeedsCommit:        feeds,
		PluginsCommit:      plugins,
		AdoptedPatchIDs:    append([]string{}, input.Resolved.AdoptedPatchIDs...),
		ResolvedConfigPath: input.ResolvedConfigPath,
		FirmwarePaths:      firmware,
		DockerSummaryPath:  input.DockerSummaryPath,
		DockerEnvironment:  dockerSummary,
	}
}

func readDockerSummary(path string) *dockerexec.Summary {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var summary dockerexec.Summary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil
	}
	return &summary
}

func finalPaths(paths []string, staging, final string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, finalPath(path, staging, final))
	}
	return out
}

func finalPath(path, staging, final string) string {
	rel, err := filepath.Rel(staging, path)
	if err != nil {
		return path
	}
	return filepath.Join(final, rel)
}

func copyRequired(src, dst string) (string, error) {
	if src == "" {
		return "", artifactError("ARTIFACT_SOURCE_MISSING", "归档源路径为空", "检查前置阶段是否写入路径", map[string]any{"destination": dst}, nil)
	}
	if err := copyFile(src, dst); err != nil {
		return "", artifactError("ARTIFACT_COPY_ERROR", "归档文件复制失败", "检查源文件和目标目录权限", map[string]any{"source": src, "destination": dst}, err)
	}
	return dst, nil
}

func copyOptional(src, dst string) (string, error) {
	if src == "" {
		return "", nil
	}
	if err := copyFile(src, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func pluginRisks(plugins []config.ResolvedPlugin) []string {
	values := []string{}
	for _, plugin := range plugins {
		if plugin.Risk != "" {
			values = append(values, plugin.Name+":"+plugin.Risk)
		}
	}
	sort.Strings(values)
	return values
}

type dockerExecRunner struct{}

func (dockerExecRunner) Run(ctx context.Context, args ...string) source.GitResult {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := source.GitResult{Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
	if err == nil {
		return result
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	} else {
		result.ExitCode = -1
	}
	return result
}

func dockerHelperArgs(resolved *config.ResolvedConfig, mounts []string, command string) []string {
	args := []string{"run", "--rm"}
	if resolved.Docker.Platform != "" && resolved.Docker.Platform != "auto" {
		args = append(args, "--platform", resolved.Docker.Platform)
	}
	args = append(args, mounts...)
	args = append(args, resolved.Docker.Image, "sh", "-lc", command)
	return args
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func artifactError(code, message, suggestion string, details map[string]any, err error) *Error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{Code: code, Message: message, Suggestion: suggestion, Details: details, Err: err}
}

func AsError(err error) (*Error, bool) {
	var artifactErr *Error
	ok := errors.As(err, &artifactErr)
	return artifactErr, ok
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return workspace.AtomicWriteFile(path, data, 0o644)
}

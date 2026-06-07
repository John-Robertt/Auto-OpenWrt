package dockerexec

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/John-Robertt/Auto-OpenWrt/internal/config"
	"github.com/John-Robertt/Auto-OpenWrt/internal/source"
	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

const SchemaVersion = 1

type Executor interface {
	Build(context.Context, Input) (*Result, error)
}

type Runner interface {
	Run(context.Context, []string, string) RunResult
	Output(context.Context, []string) string
}

type Input struct {
	ProjectRoot     string
	Resolved        *config.ResolvedConfig
	Manifest        source.WorktreeManifest
	AttachSummary   source.AttachSummary
	DownloadCache   string
	BuildCache      string
	ArtifactStaging string
	LogPath         string
	SummaryPath     string
}

type Result struct {
	LogPath     string
	SummaryPath string
	Summary     Summary
}

type Summary struct {
	SchemaVersion         int      `json:"schema_version"`
	WorkspaceID           string   `json:"workspace_id"`
	SourceSetID           string   `json:"source_set_id"`
	BuildID               string   `json:"build_id"`
	RunID                 string   `json:"run_id"`
	Image                 string   `json:"image"`
	Platform              string   `json:"platform"`
	ContainerID           string   `json:"container_id,omitempty"`
	DockerInvocationID    string   `json:"docker_invocation_id,omitempty"`
	WorktreeStorageDriver string   `json:"worktree_storage_driver"`
	VolumeNames           []string `json:"volume_names"`
	Mounts                []Mount  `json:"mounts"`
	CachePaths            []string `json:"cache_paths"`
	ArtifactStagingPath   string   `json:"artifact_staging_path"`
	DockerCLIVersion      string   `json:"docker_cli_version"`
	DockerDaemonVersion   string   `json:"docker_daemon_version"`
	Commands              []string `json:"commands"`
	StartedAt             string   `json:"started_at"`
	EndedAt               string   `json:"ended_at"`
	ExitCode              int      `json:"exit_code"`
}

type Mount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
	Kind     string `json:"kind"`
}

type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

func (r RunResult) Success() bool {
	return r.Err == nil
}

type ErrorKind string

const (
	KindStartup ErrorKind = "startup"
	KindBuild   ErrorKind = "build"
	KindWrite   ErrorKind = "write"
)

type Error struct {
	Kind       ErrorKind
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

type DefaultExecutor struct {
	Runner Runner
	Now    func() time.Time
}

func (e DefaultExecutor) Build(ctx context.Context, input Input) (*Result, error) {
	if input.Resolved == nil {
		return nil, dockerError(KindWrite, "DOCKER_EXECUTOR_INPUT_ERROR", "resolved config 为空", "先完成 config.resolve 阶段", nil, nil)
	}
	runner := e.Runner
	if runner == nil {
		runner = CLI{}
	}
	now := time.Now().UTC()
	if e.Now != nil {
		now = e.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(input.LogPath), 0o755); err != nil {
		return nil, dockerError(KindWrite, "DOCKER_LOG_WRITE_ERROR", "Docker 构建日志无法写入", "检查 run record 日志目录权限", map[string]any{"path": input.LogPath}, err)
	}
	if err := os.MkdirAll(input.ArtifactStaging, 0o755); err != nil {
		return nil, dockerError(KindWrite, "ARTIFACT_STAGING_WRITE_ERROR", "artifact staging 目录无法创建", "检查 workspace 权限和磁盘空间", map[string]any{"path": input.ArtifactStaging}, err)
	}

	mounts, volumes, err := mounts(input)
	if err != nil {
		return nil, err
	}
	commands := buildCommands(input.Resolved.Build.Jobs)
	args := dockerRunArgs(input.Resolved, mounts, commands)
	invocationID := fmt.Sprintf("%s-%s", input.Resolved.RunID, now.Format("150405"))
	summary := Summary{
		SchemaVersion:         SchemaVersion,
		WorkspaceID:           input.Resolved.WorkspaceID,
		SourceSetID:           input.Resolved.SourceSetID,
		BuildID:               input.Resolved.BuildID,
		RunID:                 input.Resolved.RunID,
		Image:                 input.Resolved.Docker.Image,
		Platform:              input.Resolved.Docker.Platform,
		DockerInvocationID:    invocationID,
		WorktreeStorageDriver: input.Manifest.StorageDriver,
		VolumeNames:           volumes,
		Mounts:                mounts,
		CachePaths:            []string{input.DownloadCache, input.BuildCache},
		ArtifactStagingPath:   input.ArtifactStaging,
		DockerCLIVersion:      runner.Output(ctx, []string{"version", "--format", "{{.Client.Version}}"}),
		DockerDaemonVersion:   runner.Output(ctx, []string{"version", "--format", "{{.Server.Version}}"}),
		Commands:              commands,
		StartedAt:             now.Format(time.RFC3339),
		ExitCode:              -1,
	}

	logHeader := dockerLogHeader(args, commands)
	if err := workspace.AtomicWriteFile(input.LogPath, []byte(logHeader), 0o644); err != nil {
		return nil, dockerError(KindWrite, "DOCKER_LOG_WRITE_ERROR", "Docker 构建日志无法写入", "检查 run record 日志目录权限", map[string]any{"path": input.LogPath}, err)
	}
	result := runner.Run(ctx, args, input.LogPath)
	ended := time.Now().UTC()
	if e.Now != nil {
		ended = e.Now().UTC()
	}
	summary.EndedAt = ended.Format(time.RFC3339)
	summary.ExitCode = result.ExitCode
	if result.Success() {
		summary.ContainerID = strings.TrimSpace(result.Stdout)
	}
	if err := writeJSON(input.SummaryPath, summary); err != nil {
		return nil, dockerError(KindWrite, "DOCKER_SUMMARY_WRITE_ERROR", "Docker 环境摘要无法写入", "检查 run record 目录权限", map[string]any{"path": input.SummaryPath}, err)
	}
	out := &Result{LogPath: input.LogPath, SummaryPath: input.SummaryPath, Summary: summary}
	if result.Success() {
		return out, nil
	}
	details := map[string]any{"exit_code": result.ExitCode, "log": input.LogPath, "docker_summary": input.SummaryPath}
	if result.ExitCode == -1 {
		return out, dockerError(KindStartup, "DOCKER_START_FAILED", "Docker 容器无法启动", "检查 docker.image、Docker daemon、权限和平台参数后重试", details, result.Err)
	}
	return out, dockerError(KindBuild, "OPENWRT_BUILD_FAILED", "OpenWrt 构建命令失败", "查看 docker-build.log 中的失败包和 make 输出后修复", details, result.Err)
}

type CLI struct{}

func (CLI) Run(ctx context.Context, args []string, logPath string) RunResult {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout bytes.Buffer
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return RunResult{ExitCode: -1, Err: err}
	}
	defer logFile.Close()
	cmd.Stdout = multiLineWriter(logFile, &stdout)
	cmd.Stderr = logFile
	err = cmd.Run()
	result := RunResult{Stdout: stdout.String(), Err: err}
	if err == nil {
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	} else {
		result.ExitCode = -1
	}
	return result
}

func (CLI) Output(ctx context.Context, args []string) string {
	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func mounts(input Input) ([]Mount, []string, error) {
	out := []Mount{}
	volumes := []string{}
	if input.Manifest.StorageDriver == "docker-volume" {
		if input.Manifest.DockerVolumeName == "" {
			return nil, nil, dockerError(KindWrite, "DOCKER_MAPPING_ERROR", "Docker volume 映射缺失", "重新准备运行工作树", map[string]any{"storage_driver": input.Manifest.StorageDriver}, nil)
		}
		out = append(out, Mount{Source: input.Manifest.DockerVolumeName, Target: "/openwrt", Kind: "volume"})
		volumes = append(volumes, input.Manifest.DockerVolumeName)
	} else {
		if input.Manifest.PhysicalSourcePath == "" {
			return nil, nil, dockerError(KindWrite, "DOCKER_MAPPING_ERROR", "宿主工作树映射缺失", "重新准备运行工作树", map[string]any{"storage_driver": input.Manifest.StorageDriver}, nil)
		}
		out = append(out, Mount{Source: input.Manifest.PhysicalSourcePath, Target: "/openwrt", Kind: "bind"})
	}
	out = append(out,
		Mount{Source: input.DownloadCache, Target: "/openwrt/dl", Kind: "bind"},
		Mount{Source: input.BuildCache, Target: "/auto-openwrt/cache/build", Kind: "bind"},
		Mount{Source: input.ArtifactStaging, Target: "/auto-openwrt/artifacts", Kind: "bind"},
	)
	for _, entry := range input.AttachSummary.Feeds {
		if entry.SourcePath == "" {
			continue
		}
		out = append(out, Mount{Source: entry.SourcePath, Target: "/auto-openwrt/sources/" + input.Resolved.SourceSetID + "/feeds/" + entry.Name, ReadOnly: true, Kind: "bind"})
	}
	for _, entry := range input.AttachSummary.Plugins {
		if entry.Type != "feed" || entry.SourcePath == "" {
			continue
		}
		out = append(out, Mount{Source: entry.SourcePath, Target: "/auto-openwrt/sources/" + input.Resolved.SourceSetID + "/plugins/" + entry.Name, ReadOnly: true, Kind: "bind"})
	}
	return out, volumes, nil
}

func dockerRunArgs(resolved *config.ResolvedConfig, mounts []Mount, commands []string) []string {
	args := []string{"run", "--rm"}
	if resolved.Docker.Platform != "" && resolved.Docker.Platform != "auto" {
		args = append(args, "--platform", resolved.Docker.Platform)
	}
	for _, mount := range mounts {
		spec := mount.Source + ":" + mount.Target
		if mount.ReadOnly {
			spec += ":ro"
		}
		args = append(args, "-v", spec)
	}
	args = append(args, "-w", "/openwrt", resolved.Docker.Image, "sh", "-lc", strings.Join(commands, "\n"))
	return args
}

func buildCommands(jobs int) []string {
	if jobs <= 0 {
		jobs = 1
	}
	return []string{
		"./scripts/feeds update -a",
		"./scripts/feeds install -a",
		"if ls /openwrt/.auto-openwrt/config-fragments/*.config >/dev/null 2>&1; then cat /openwrt/.auto-openwrt/config-fragments/*.config >> .config; fi",
		"make defconfig",
		fmt.Sprintf("make -j%d V=s", jobs),
	}
}

func dockerLogHeader(args []string, commands []string) string {
	var builder strings.Builder
	builder.WriteString("$ docker ")
	builder.WriteString(strings.Join(args, " "))
	builder.WriteString("\n")
	for _, command := range commands {
		builder.WriteString("$ ")
		builder.WriteString(command)
		builder.WriteString("\n")
	}
	return builder.String()
}

func multiLineWriter(logFile *os.File, stdout *bytes.Buffer) *prefixedWriter {
	return &prefixedWriter{log: logFile, stdout: stdout}
}

type prefixedWriter struct {
	log    *os.File
	stdout *bytes.Buffer
}

func (w *prefixedWriter) Write(p []byte) (int, error) {
	if _, err := w.stdout.Write(p); err != nil {
		return 0, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(p))
	for scanner.Scan() {
		if _, err := w.log.WriteString(scanner.Text() + "\n"); err != nil {
			return 0, err
		}
	}
	return len(p), scanner.Err()
}

func dockerError(kind ErrorKind, code, message, suggestion string, details map[string]any, err error) *Error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{Kind: kind, Code: code, Message: message, Suggestion: suggestion, Details: details, Err: err}
}

func AsError(err error) (*Error, bool) {
	var dockerErr *Error
	ok := errors.As(err, &dockerErr)
	return dockerErr, ok
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return workspace.AtomicWriteFile(path, data, 0o644)
}

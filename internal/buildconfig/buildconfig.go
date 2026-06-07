package buildconfig

import (
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

type Generator interface {
	Generate(context.Context, Input) (*Result, error)
}

type Input struct {
	ProjectRoot string
	Resolved    *config.ResolvedConfig
	Manifest    source.WorktreeManifest
	RunDir      string
}

type Result struct {
	FragmentDir string
	Fragments   []string
	SummaryPath string
}

type Summary struct {
	SchemaVersion  int      `json:"schema_version"`
	WorkspaceID    string   `json:"workspace_id"`
	SourceSetID    string   `json:"source_set_id"`
	BuildID        string   `json:"build_id"`
	RunID          string   `json:"run_id"`
	FragmentDir    string   `json:"fragment_dir"`
	Fragments      []string `json:"fragments"`
	HostStagingDir string   `json:"host_staging_dir,omitempty"`
	CreatedAt      string   `json:"created_at"`
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

type DefaultGenerator struct {
	Docker source.DockerRunner
	Now    func() time.Time
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

func (g DefaultGenerator) Generate(ctx context.Context, input Input) (*Result, error) {
	_ = ctx
	if input.Resolved == nil {
		return nil, buildConfigError("BUILD_CONFIG_ERROR", "resolved config 为空", "先完成 config.resolve 阶段", nil, nil)
	}
	fragmentDir := ""
	hostStagingDir := ""
	if input.Manifest.StorageDriver == "docker-volume" {
		if input.Manifest.DockerVolumeName == "" {
			return nil, buildConfigError("BUILD_CONFIG_WORKTREE_MISSING", "Docker volume 未记录", "重新运行 build 以准备运行工作树", map[string]any{"storage_driver": input.Manifest.StorageDriver}, nil)
		}
		hostStagingDir = filepath.Join(input.RunDir, "build-config-fragments")
		fragmentDir = hostStagingDir
	} else if input.Manifest.PhysicalSourcePath == "" {
		return nil, buildConfigError("BUILD_CONFIG_WORKTREE_MISSING", "工作树物理路径为空", "重新运行 build 以准备运行工作树", map[string]any{"storage_driver": input.Manifest.StorageDriver}, nil)
	} else {
		fragmentDir = filepath.Join(input.Manifest.PhysicalSourcePath, ".auto-openwrt", "config-fragments")
	}

	if err := os.MkdirAll(fragmentDir, 0o755); err != nil {
		return nil, buildConfigError("BUILD_CONFIG_WRITE_ERROR", "构建配置片段目录无法创建", "检查当前 run 工作树权限", map[string]any{"path": fragmentDir}, err)
	}

	fragments := []string{}
	targetPath := filepath.Join(fragmentDir, "00-target.config")
	if err := workspace.AtomicWriteFile(targetPath, []byte(targetFragment(input.Resolved)), 0o644); err != nil {
		return nil, buildConfigError("BUILD_CONFIG_WRITE_ERROR", "OpenWrt target 配置片段无法写入", "检查当前 run 工作树权限", map[string]any{"path": targetPath}, err)
	}
	fragments = append(fragments, targetPath)

	for i, rel := range input.Resolved.Build.Fragments {
		src := filepath.Join(input.ProjectRoot, filepath.FromSlash(rel))
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, buildConfigError("BUILD_CONFIG_FRAGMENT_MISSING", "构建配置 fragment 不存在或不可读", "检查 builds[].config.fragments 中的 project-root-relative 路径", map[string]any{"fragment": rel, "path": src}, err)
		}
		dst := filepath.Join(fragmentDir, fmt.Sprintf("10-user-%03d.config", i))
		if err := workspace.AtomicWriteFile(dst, data, 0o644); err != nil {
			return nil, buildConfigError("BUILD_CONFIG_WRITE_ERROR", "用户配置 fragment 无法复制到当前 run 工作树", "检查当前 run 工作树权限", map[string]any{"path": dst}, err)
		}
		fragments = append(fragments, dst)
	}

	if len(input.Resolved.Build.Packages) > 0 {
		packagesPath := filepath.Join(fragmentDir, "20-packages.config")
		if err := workspace.AtomicWriteFile(packagesPath, []byte(packagesFragment(input.Resolved.Build.Packages)), 0o644); err != nil {
			return nil, buildConfigError("BUILD_CONFIG_WRITE_ERROR", "package 配置片段无法写入", "检查当前 run 工作树权限", map[string]any{"path": packagesPath}, err)
		}
		fragments = append(fragments, packagesPath)
	}

	now := time.Now().UTC()
	if g.Now != nil {
		now = g.Now().UTC()
	}
	summaryPath := filepath.Join(input.RunDir, "build-config-summary.json")
	summary := Summary{
		SchemaVersion:  SchemaVersion,
		WorkspaceID:    input.Resolved.WorkspaceID,
		SourceSetID:    input.Resolved.SourceSetID,
		BuildID:        input.Resolved.BuildID,
		RunID:          input.Resolved.RunID,
		FragmentDir:    fragmentDir,
		Fragments:      fragments,
		HostStagingDir: hostStagingDir,
		CreatedAt:      now.Format(time.RFC3339),
	}
	resultFragmentDir := fragmentDir
	if input.Manifest.StorageDriver == "docker-volume" {
		if err := g.copyFragmentsToDockerVolume(ctx, input, fragmentDir); err != nil {
			return nil, err
		}
		resultFragmentDir = "/openwrt/.auto-openwrt/config-fragments"
		summary.FragmentDir = resultFragmentDir
	}
	if err := writeJSON(summaryPath, summary); err != nil {
		return nil, buildConfigError("BUILD_CONFIG_WRITE_ERROR", "构建配置摘要无法写入", "检查 run record 目录权限", map[string]any{"path": summaryPath}, err)
	}
	return &Result{FragmentDir: resultFragmentDir, Fragments: fragments, SummaryPath: summaryPath}, nil
}

func (g DefaultGenerator) copyFragmentsToDockerVolume(ctx context.Context, input Input, hostFragmentDir string) error {
	runner := g.Docker
	if runner == nil {
		runner = dockerExecRunner{}
	}
	command := "mkdir -p /openwrt/.auto-openwrt/config-fragments && rm -f /openwrt/.auto-openwrt/config-fragments/*.config && cp -a /auto-openwrt/config-fragments/. /openwrt/.auto-openwrt/config-fragments/"
	args := dockerHelperArgs(input.Resolved, []string{"-v", input.Manifest.DockerVolumeName + ":/openwrt", "-v", hostFragmentDir + ":/auto-openwrt/config-fragments:ro"}, command)
	if result := runner.Run(ctx, args...); !result.Success() {
		return buildConfigError("BUILD_CONFIG_WRITE_ERROR", "docker-volume 构建配置片段无法写入", "检查 docker.image 是否具备 sh/cp，并确认 Docker volume 可写", map[string]any{"stderr": strings.TrimSpace(result.Stderr)}, result.Err)
	}
	return nil
}

func targetFragment(resolved *config.ResolvedConfig) string {
	target := sanitizeToken(resolved.Build.Target)
	subtarget := sanitizeToken(resolved.Build.Subtarget)
	profile := sanitizeToken(resolved.Build.Profile)
	lines := []string{
		"CONFIG_TARGET_" + target + "=y",
		"CONFIG_TARGET_" + target + "_" + subtarget + "=y",
		"CONFIG_TARGET_" + target + "_" + subtarget + "_DEVICE_" + profile + "=y",
		"",
	}
	return strings.Join(lines, "\n")
}

func packagesFragment(packages []string) string {
	lines := make([]string, 0, len(packages)+1)
	for _, name := range packages {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, "-") {
			pkg := sanitizePackage(strings.TrimPrefix(name, "-"))
			if pkg != "" {
				lines = append(lines, "# CONFIG_PACKAGE_"+pkg+" is not set")
			}
			continue
		}
		lines = append(lines, "CONFIG_PACKAGE_"+sanitizePackage(name)+"=y")
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

func sanitizeToken(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, ".", "_")
	return value
}

func sanitizePackage(value string) string {
	return sanitizeToken(value)
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

func buildConfigError(code, message, suggestion string, details map[string]any, err error) *Error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{Code: code, Message: message, Suggestion: suggestion, Details: details, Err: err}
}

func AsError(err error) (*Error, bool) {
	var cfgErr *Error
	ok := errors.As(err, &cfgErr)
	return cfgErr, ok
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return workspace.AtomicWriteFile(path, data, 0o644)
}

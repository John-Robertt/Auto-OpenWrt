package health

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/John-Robertt/Auto-OpenWrt/internal/config"
	"github.com/John-Robertt/Auto-OpenWrt/internal/source"
	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

const SchemaVersion = 1

type Status string

const (
	Pass Status = "pass"
	Warn Status = "warn"
	Fail Status = "fail"
)

type Item struct {
	ID         string         `json:"id"`
	Status     Status         `json:"status"`
	Summary    string         `json:"summary"`
	Detail     string         `json:"detail"`
	Suggestion string         `json:"suggestion"`
	Details    map[string]any `json:"details,omitempty"`
}

type Report struct {
	SchemaVersion         int      `json:"schema_version"`
	CheckedAt             string   `json:"checked_at"`
	RunID                 string   `json:"run_id"`
	WorkspaceID           *string  `json:"workspace_id"`
	SourceSetID           *string  `json:"source_set_id"`
	BuildID               *string  `json:"build_id"`
	ProjectRoot           string   `json:"project_root"`
	Preflight             []Item   `json:"preflight"`
	BuildContext          []Item   `json:"build_context"`
	DockerImage           string   `json:"docker_image"`
	DockerPlatform        string   `json:"docker_platform"`
	WorktreeStorageDriver string   `json:"worktree_storage_driver"`
	LogicalWorktreeID     string   `json:"logical_worktree_id"`
	PhysicalWorktreePath  string   `json:"physical_worktree_path,omitempty"`
	DockerVolumeName      string   `json:"docker_volume_name,omitempty"`
	PluginRisks           []string `json:"plugin_risks"`
	AIRepairAvailable     bool     `json:"ai_repair_available"`
	CanContinue           bool     `json:"can_continue"`
}

type PreflightInput struct {
	RunID       string
	ProjectRoot string
	ConfigPath  string
	WorkspaceID *string
	SourceSetID *string
	BuildID     *string
	Config      *config.UserConfig
	Resolved    *config.ResolvedConfig
}

type Checker interface {
	Preflight(context.Context, PreflightInput) (*Report, error)
	BuildContext(context.Context, BuildContextInput) (*Report, error)
}

type BuildContextInput struct {
	RunID         string
	ProjectRoot   string
	WorkspaceID   string
	SourceSetID   string
	BuildID       string
	Resolved      *config.ResolvedConfig
	Manifest      source.WorktreeManifest
	ManifestPath  string
	AttachSummary *source.AttachSummary
	Existing      *Report
}

type DefaultChecker struct {
	Probe  Probe
	Docker source.DockerRunner
	Now    func() time.Time
}

type Probe interface {
	LookPath(file string) (string, error)
	CommandSucceeds(ctx context.Context, name string, args ...string) error
	PathWritable(path string) error
	DiskAvailableGB(path string) (uint64, error)
	GOOS() string
	GOARCH() string
}

type OSProbe struct{}

func (c DefaultChecker) Preflight(ctx context.Context, input PreflightInput) (*Report, error) {
	probe := c.Probe
	if probe == nil {
		probe = OSProbe{}
	}
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now().UTC()
	}

	report := &Report{
		SchemaVersion: SchemaVersion,
		CheckedAt:     now.Format(time.RFC3339),
		RunID:         input.RunID,
		WorkspaceID:   input.WorkspaceID,
		SourceSetID:   input.SourceSetID,
		BuildID:       input.BuildID,
		ProjectRoot:   input.ProjectRoot,
		BuildContext:  []Item{},
		CanContinue:   true,
	}

	if input.Resolved != nil {
		report.DockerImage = input.Resolved.Docker.Image
		report.DockerPlatform = input.Resolved.Docker.Platform
		report.WorktreeStorageDriver = input.Resolved.Workspace.WorktreeStorage
		report.LogicalWorktreeID = input.Resolved.Workspace.LogicalWorktreeID
		report.PluginRisks = pluginRisks(input.Resolved.Plugins)
	} else if input.Config != nil {
		report.DockerImage = input.Config.Docker.Image
		report.DockerPlatform = defaultString(input.Config.Docker.Platform, "auto")
		report.WorktreeStorageDriver = defaultString(input.Config.Workspace.WorktreeStorage, "auto")
		report.PluginRisks = configPluginRisks(input.Config.Plugins)
	}

	report.Preflight = append(report.Preflight,
		passItem("system.os", "宿主系统已识别", probe.GOOS()+"/"+probe.GOARCH(), "无需操作"),
		commandItem(probe, "system.commands", []string{"git", "sh", "docker"}),
		warnItem("network.git", "未执行远端 Git 网络访问", "D2 doctor 不主动访问远端仓库，后续 update/build 阶段会验证具体仓库", "如果后续源码更新失败，检查网络、代理和仓库 URL"),
	)

	dockerPath, dockerErr := probe.LookPath("docker")
	if dockerErr != nil {
		report.Preflight = append(report.Preflight,
			failItem("docker.installed", "Docker CLI 不可用", "未在 PATH 中找到 docker", "安装 Docker 并确认 docker 命令可执行"),
			failItem("docker.running", "Docker daemon 不可用", "Docker CLI 缺失，无法检查 daemon", "安装并启动 Docker"),
			failItem("docker.permission", "无法验证 Docker 权限", "Docker CLI 缺失，无法检查当前用户权限", "安装 Docker 后确认当前用户可运行 docker info"),
		)
	} else {
		report.Preflight = append(report.Preflight, passItem("docker.installed", "Docker CLI 可用", dockerPath, "无需操作"))
		if err := probe.CommandSucceeds(ctx, "docker", "info"); err != nil {
			report.Preflight = append(report.Preflight,
				failItem("docker.running", "Docker daemon 不可用", err.Error(), "启动 Docker daemon 后重试"),
				failItem("docker.permission", "当前用户无法访问 Docker", err.Error(), "把当前用户加入 docker 权限组，或使用可访问 Docker 的 shell"),
			)
		} else {
			report.Preflight = append(report.Preflight,
				passItem("docker.running", "Docker daemon 可用", "docker info 成功", "无需操作"),
				passItem("docker.permission", "当前用户可访问 Docker", "docker info 成功", "无需操作"),
			)
		}
	}

	if report.DockerImage == "" {
		report.Preflight = append(report.Preflight, failItem("docker.image", "Docker image 为空", "配置中 docker.image 为空", "设置 docker.image"))
	} else {
		report.Preflight = append(report.Preflight, passItem("docker.image", "Docker image 已声明", report.DockerImage, "无需操作"))
	}
	if report.DockerPlatform == "" || report.DockerPlatform == "auto" {
		report.Preflight = append(report.Preflight, passItem("docker.platform", "Docker platform 使用 auto", "不向 docker run 传递 --platform", "无需操作"))
	} else {
		report.Preflight = append(report.Preflight, passItem("docker.platform", "Docker platform 已声明", report.DockerPlatform, "确认 Docker 支持该 platform"))
	}

	report.Preflight = append(report.Preflight,
		pathWritableItem(probe, "project.read_write", input.ProjectRoot, "project root 可读写", "project root 不可写"),
		projectDirectoriesItem(probe, input.ProjectRoot),
	)
	if input.WorkspaceID != nil {
		workspacePath := filepath.Join(input.ProjectRoot, "workspaces", *input.WorkspaceID)
		report.Preflight = append(report.Preflight, pathWritableItem(probe, "workspace.read_write", workspacePath, "workspace 状态目录可读写", "workspace 状态目录不可写"))
	} else {
		report.Preflight = append(report.Preflight, passItem("workspace.read_write", "未绑定 workspace", "project-level doctor 不检查 workspace 状态目录", "无需操作"))
	}
	report.Preflight = append(report.Preflight,
		passItem("workspace.storage_driver", "工作树存储策略已解析", defaultString(report.WorktreeStorageDriver, "auto"), "无需操作"),
		pathWritableItem(probe, "cache.read_write", filepath.Join(input.ProjectRoot, "cache"), "cache 目录可读写", "cache 目录不可写"),
		diskItem(probe, input.ProjectRoot, minDiskGB(input.Config)),
		aiCommandItem(probe, input.Config),
	)

	report.CanContinue = canContinue(report.Preflight)
	return report, nil
}

func (c DefaultChecker) BuildContext(ctx context.Context, input BuildContextInput) (*Report, error) {
	probe := c.Probe
	if probe == nil {
		probe = OSProbe{}
	}
	report := input.Existing
	if report == nil {
		report = &Report{
			SchemaVersion: SchemaVersion,
			RunID:         input.RunID,
			ProjectRoot:   input.ProjectRoot,
			Preflight:     []Item{},
		}
	}
	report.WorkspaceID = stringPtr(input.WorkspaceID)
	report.SourceSetID = stringPtr(input.SourceSetID)
	report.BuildID = stringPtr(input.BuildID)
	report.WorktreeStorageDriver = input.Manifest.StorageDriver
	report.LogicalWorktreeID = input.Manifest.LogicalWorktreeID
	report.PhysicalWorktreePath = input.Manifest.PhysicalSourcePath
	report.DockerVolumeName = input.Manifest.DockerVolumeName
	if input.Resolved != nil {
		report.DockerImage = input.Resolved.Docker.Image
		report.DockerPlatform = input.Resolved.Docker.Platform
		report.PluginRisks = pluginRisks(input.Resolved.Plugins)
		report.AIRepairAvailable = !input.Resolved.AIRepair.Enabled || input.Manifest.StorageDriver != "docker-volume"
	}

	items := []Item{
		manifestItem(input.ManifestPath, input.Manifest),
		worktreeExistsItem(input.Manifest),
		worktreeWritableItem(probe, input.Manifest),
		filesystemItem(input.Manifest),
		openWrtTargetItem(ctx, c.Docker, input.Resolved, input.Manifest),
		dockerMappingItem(input.Resolved, input.Manifest),
		dockerMountScopeItem(input.ProjectRoot, input.Manifest, input.AttachSummary),
		pluginRiskItem(input.AttachSummary),
		pluginAttachItem(input.AttachSummary),
		aiWorktreeAccessItem(input.Resolved, input.Manifest),
	}
	report.BuildContext = items
	report.CanContinue = canContinue(report.Preflight) && canContinue(report.BuildContext)
	return report, nil
}

func WriteReport(path string, report *Report) error {
	if report == nil {
		return errors.New("health report is nil")
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return workspace.AtomicWriteFile(path, data, 0o644)
}

func manifestItem(path string, manifest source.WorktreeManifest) Item {
	if path == "" {
		return failItem("worktree.manifest", "worktree manifest 缺失", "manifest path 为空", "重新运行 build 以准备运行工作树")
	}
	if _, err := os.Stat(path); err != nil {
		return failItem("worktree.manifest", "worktree manifest 不可读", err.Error(), "重新运行 build，或检查 run record 目录权限")
	}
	if manifest.SchemaVersion != SchemaVersion {
		return failItem("worktree.manifest", "worktree manifest schema_version 不匹配", "schema_version 非 1", "清理该 run 后重新运行 build")
	}
	return passItem("worktree.manifest", "worktree manifest 可用", path, "无需操作")
}

func worktreeExistsItem(manifest source.WorktreeManifest) Item {
	if manifest.StorageDriver == "docker-volume" {
		if manifest.DockerVolumeName == "" {
			return failItem("worktree.exists", "Docker volume 未记录", "manifest 缺少 docker_volume_name", "重新准备运行工作树")
		}
		return passItem("worktree.exists", "Docker volume 工作树已记录", manifest.DockerVolumeName, "无需操作")
	}
	if manifest.PhysicalSourcePath == "" {
		return failItem("worktree.exists", "工作树物理路径为空", "manifest 缺少 physical_source_path", "重新准备运行工作树")
	}
	info, err := os.Stat(manifest.PhysicalSourcePath)
	if err != nil {
		return failItem("worktree.exists", "工作树不存在", err.Error(), "重新运行 build 以创建当前 run 工作树")
	}
	if !info.IsDir() {
		return failItem("worktree.exists", "工作树路径不是目录", manifest.PhysicalSourcePath, "清理该路径后重新运行 build")
	}
	return passItem("worktree.exists", "工作树存在", manifest.PhysicalSourcePath, "无需操作")
}

func worktreeWritableItem(probe Probe, manifest source.WorktreeManifest) Item {
	if manifest.StorageDriver == "docker-volume" {
		return passItem("worktree.read_write", "Docker volume 工作树由 Docker 管理", manifest.DockerVolumeName, "无需操作")
	}
	return pathWritableItem(probe, "worktree.read_write", manifest.PhysicalSourcePath, "当前 run 工作树可读写", "当前 run 工作树不可写")
}

func filesystemItem(manifest source.WorktreeManifest) Item {
	if manifest.StorageDriver == "host-path" && !manifest.CaseSensitive {
		return warnItem("worktree.filesystem", "host-path 文件系统大小写不敏感", manifest.FilesystemRisk, "切换到 docker-volume 或 linux-path")
	}
	return passItem("worktree.filesystem", "工作树文件系统满足 D4 校验", manifest.StorageDriver, "无需操作")
}

func openWrtTargetItem(ctx context.Context, runner source.DockerRunner, resolved *config.ResolvedConfig, manifest source.WorktreeManifest) Item {
	if resolved == nil {
		return failItem("openwrt.target", "resolved config 缺失", "无法校验 OpenWrt target", "先完成 config.resolve")
	}
	if manifest.StorageDriver == "docker-volume" {
		return openWrtTargetDockerVolumeItem(ctx, runner, resolved, manifest)
	}
	root := manifest.PhysicalSourcePath
	targetDir := filepath.Join(root, "target", "linux", resolved.Build.Target)
	subtargetDir := filepath.Join(targetDir, resolved.Build.Subtarget)
	if info, err := os.Stat(targetDir); err != nil || !info.IsDir() {
		return failItem("openwrt.target", "OpenWrt target 不存在", targetDir, "检查 builds[].openwrt.target")
	}
	if info, err := os.Stat(subtargetDir); err != nil || !info.IsDir() {
		return failItem("openwrt.target", "OpenWrt subtarget 不存在", subtargetDir, "检查 builds[].openwrt.subtarget")
	}
	if !profileExists(targetDir, resolved.Build.Profile) {
		return failItem("openwrt.target", "OpenWrt device profile 不存在", resolved.Build.Profile, "检查 builds[].openwrt.profile")
	}
	return passItem("openwrt.target", "OpenWrt target/subtarget/profile 有效", resolved.Build.Target+"/"+resolved.Build.Subtarget+"/"+resolved.Build.Profile, "无需操作")
}

func openWrtTargetDockerVolumeItem(ctx context.Context, runner source.DockerRunner, resolved *config.ResolvedConfig, manifest source.WorktreeManifest) Item {
	if manifest.DockerVolumeName == "" {
		return failItem("openwrt.target", "Docker volume 未记录", "manifest 缺少 docker_volume_name", "重新准备运行工作树")
	}
	if runner == nil {
		runner = dockerExecRunner{}
	}
	targetDir := "/openwrt/target/linux/" + resolved.Build.Target
	subtargetDir := targetDir + "/" + resolved.Build.Subtarget
	needleA := "DEVICE_" + resolved.Build.Profile
	needleB := "Device/" + resolved.Build.Profile
	command := "test -d " + shellQuote(targetDir) + " && test -d " + shellQuote(subtargetDir) + " && grep -R -q -e " + shellQuote(needleA) + " -e " + shellQuote(needleB) + " " + shellQuote(targetDir)
	args := dockerHelperArgs(resolved, []string{"-v", manifest.DockerVolumeName + ":/openwrt"}, command)
	if result := runner.Run(ctx, args...); !result.Success() {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = resolved.Build.Target + "/" + resolved.Build.Subtarget + "/" + resolved.Build.Profile
		}
		return failItem("openwrt.target", "OpenWrt target/subtarget/profile 不存在", detail, "检查 builds[].openwrt target、subtarget 和 profile")
	}
	return passItem("openwrt.target", "OpenWrt target/subtarget/profile 有效", resolved.Build.Target+"/"+resolved.Build.Subtarget+"/"+resolved.Build.Profile, "无需操作")
}

func dockerMappingItem(resolved *config.ResolvedConfig, manifest source.WorktreeManifest) Item {
	if resolved == nil {
		return failItem("docker.mapping", "Docker 映射无法解析", "resolved config 缺失", "先完成 config.resolve")
	}
	if resolved.Docker.Image == "" {
		return failItem("docker.mapping", "Docker image 为空", "resolved docker.image 为空", "设置 docker.image")
	}
	if manifest.StorageDriver == "docker-volume" && manifest.DockerVolumeName == "" {
		return failItem("docker.mapping", "Docker volume 映射缺失", "manifest 未记录 volume", "重新准备运行工作树")
	}
	if manifest.StorageDriver != "docker-volume" && manifest.PhysicalSourcePath == "" {
		return failItem("docker.mapping", "宿主工作树映射缺失", "manifest 未记录 physical_source_path", "重新准备运行工作树")
	}
	return passItem("docker.mapping", "Docker 映射信息明确", resolved.Docker.Image+" -> "+manifest.ContainerPath, "无需操作")
}

func dockerMountScopeItem(projectRoot string, manifest source.WorktreeManifest, attach *source.AttachSummary) Item {
	if manifest.PhysicalSourcePath != "" && sameCleanPath(projectRoot, manifest.PhysicalSourcePath) {
		return failItem("docker.mount_scope", "Docker mount 范围包含 project root", manifest.PhysicalSourcePath, "只挂载当前 run 工作树")
	}
	if attach != nil {
		for _, entry := range append(append([]source.AttachEntry{}, attach.Feeds...), attach.Plugins...) {
			if sameCleanPath(projectRoot, entry.SourcePath) || sameCleanPath(projectRoot, entry.TargetPath) {
				return failItem("docker.mount_scope", "Docker mount 范围包含 project root", entry.SourcePath, "只挂载当前 run 工作树、缓存、artifact staging 和必要 source-set cache")
			}
		}
	}
	return passItem("docker.mount_scope", "Docker mount 范围未包含 project root", "D4 仅记录允许映射材料", "无需操作")
}

func pluginRiskItem(attach *source.AttachSummary) Item {
	if attach == nil {
		return failItem("plugins.risk", "插件风险信息缺失", "attach summary 为空", "先完成 plugins.attach")
	}
	return passItem("plugins.risk", "插件风险已写入上下文", "plugins: "+itoa(len(attach.Plugins)), "无需操作")
}

func pluginAttachItem(attach *source.AttachSummary) Item {
	if attach == nil {
		return failItem("plugins.attach", "插件接入摘要缺失", "attach summary 为空", "先完成 plugins.attach")
	}
	return passItem("plugins.attach", "feeds/plugins 接入材料存在", "feeds: "+itoa(len(attach.Feeds))+", plugins: "+itoa(len(attach.Plugins)), "无需操作")
}

func aiWorktreeAccessItem(resolved *config.ResolvedConfig, manifest source.WorktreeManifest) Item {
	if resolved == nil || !resolved.AIRepair.Enabled {
		return passItem("ai.worktree_access", "AI 修复未启用", "ai_repair.enabled=false", "无需操作")
	}
	if manifest.StorageDriver == "docker-volume" {
		return failItem("ai.worktree_access", "AI CLI 无法访问 docker-volume 工作树", "docker-volume 没有宿主源码路径", "切换 workspace.worktree_storage 到 host-path 或 linux-path")
	}
	if manifest.PhysicalSourcePath == "" {
		return failItem("ai.worktree_access", "AI CLI 工作树路径为空", "manifest 缺少宿主源码路径", "重新准备运行工作树")
	}
	return passItem("ai.worktree_access", "AI CLI 可访问当前 run 工作树路径", manifest.PhysicalSourcePath, "无需操作")
}

func profileExists(targetDir, profile string) bool {
	found := false
	needleA := "DEVICE_" + profile
	needleB := "Device/" + profile
	_ = filepath.WalkDir(targetDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || found || entry.IsDir() {
			return nil
		}
		if filepath.Ext(entry.Name()) != ".mk" && entry.Name() != "Makefile" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := string(data)
		if strings.Contains(text, needleA) || strings.Contains(text, needleB) {
			found = true
		}
		return nil
	})
	return found
}

func sameCleanPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
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

func stringPtr(value string) *string {
	return &value
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func (OSProbe) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (OSProbe) CommandSucceeds(ctx context.Context, name string, args ...string) error {
	commandCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 0 {
			return errors.New(string(output))
		}
		return err
	}
	return nil
}

func (OSProbe) PathWritable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("path is not a directory")
	}
	temp, err := os.CreateTemp(path, ".auto-openwrt-write-test-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	if closeErr := temp.Close(); closeErr != nil {
		_ = os.Remove(name)
		return closeErr
	}
	return os.Remove(name)
}

func (OSProbe) DiskAvailableGB(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize) / 1024 / 1024 / 1024, nil
}

func (OSProbe) GOOS() string {
	return runtime.GOOS
}

func (OSProbe) GOARCH() string {
	return runtime.GOARCH
}

func commandItem(probe Probe, id string, commands []string) Item {
	missing := []string{}
	found := map[string]string{}
	for _, command := range commands {
		path, err := probe.LookPath(command)
		if err != nil {
			missing = append(missing, command)
			continue
		}
		found[command] = path
	}
	if len(missing) > 0 {
		return Item{
			ID:         id,
			Status:     Fail,
			Summary:    "基础命令缺失",
			Detail:     "缺失命令: " + join(missing),
			Suggestion: "安装缺失命令并确认它们位于 PATH 中",
			Details:    map[string]any{"missing": missing, "found": found},
		}
	}
	return Item{ID: id, Status: Pass, Summary: "基础命令可用", Detail: "git、sh、docker 均可执行", Suggestion: "无需操作", Details: map[string]any{"found": found}}
}

func pathWritableItem(probe Probe, id, path, passSummary, failSummary string) Item {
	if err := probe.PathWritable(path); err != nil {
		return failItem(id, failSummary, err.Error(), "检查路径是否存在、权限是否可写以及磁盘是否可用")
	}
	return passItem(id, passSummary, path, "无需操作")
}

func projectDirectoriesItem(probe Probe, projectRoot string) Item {
	missing := []string{}
	notWritable := []string{}
	for _, rel := range workspace.RequiredDirs {
		path := filepath.Join(projectRoot, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			missing = append(missing, rel)
			continue
		}
		if err := probe.PathWritable(path); err != nil {
			notWritable = append(notWritable, rel)
		}
	}
	if len(missing) > 0 || len(notWritable) > 0 {
		return Item{
			ID:         "project.directories",
			Status:     Fail,
			Summary:    "project root 必需目录不可用",
			Detail:     "缺失或不可写目录会阻断后续构建",
			Suggestion: "重新运行 init，或修复目录权限后重试",
			Details:    map[string]any{"missing": missing, "not_writable": notWritable},
		}
	}
	return passItem("project.directories", "project root 必需目录可用", "所有必需目录存在且可写", "无需操作")
}

func diskItem(probe Probe, projectRoot string, required int) Item {
	available, err := probe.DiskAvailableGB(projectRoot)
	if err != nil {
		return failItem("disk.available", "磁盘空间无法检查", err.Error(), "检查 project root 所在文件系统")
	}
	if required > 0 && available < uint64(required) {
		return Item{
			ID:         "disk.available",
			Status:     Fail,
			Summary:    "可用磁盘空间不足",
			Detail:     "可用空间低于配置阈值",
			Suggestion: "释放磁盘空间或调低 health.min_disk_gb",
			Details:    map[string]any{"available_gb": available, "required_gb": required},
		}
	}
	return Item{ID: "disk.available", Status: Pass, Summary: "磁盘空间满足要求", Detail: "可用空间满足 health.min_disk_gb", Suggestion: "无需操作", Details: map[string]any{"available_gb": available, "required_gb": required}}
}

func aiCommandItem(probe Probe, cfg *config.UserConfig) Item {
	if cfg == nil || cfg.AIRepair.Enabled == nil || !*cfg.AIRepair.Enabled {
		return passItem("ai.command", "AI 修复未启用", "ai_repair.enabled=false", "无需操作")
	}
	if cfg.AIRepair.Command == "" {
		return failItem("ai.command", "AI CLI 未声明", "ai_repair.command 为空", "设置 ai_repair.command")
	}
	path, err := probe.LookPath(cfg.AIRepair.Command)
	if err != nil {
		return failItem("ai.command", "AI CLI 不可执行", err.Error(), "安装 AI CLI 或修正 ai_repair.command")
	}
	return passItem("ai.command", "AI CLI 可执行", path, "无需操作")
}

func passItem(id, summary, detail, suggestion string) Item {
	return Item{ID: id, Status: Pass, Summary: summary, Detail: detail, Suggestion: suggestion}
}

func warnItem(id, summary, detail, suggestion string) Item {
	return Item{ID: id, Status: Warn, Summary: summary, Detail: detail, Suggestion: suggestion}
}

func failItem(id, summary, detail, suggestion string) Item {
	return Item{ID: id, Status: Fail, Summary: summary, Detail: detail, Suggestion: suggestion}
}

func canContinue(items []Item) bool {
	for _, item := range items {
		if item.Status == Fail {
			return false
		}
	}
	return true
}

func pluginRisks(plugins []config.ResolvedPlugin) []string {
	risks := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		risks = append(risks, plugin.Risk)
	}
	return risks
}

func configPluginRisks(plugins []config.PluginConfig) []string {
	risks := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		risks = append(risks, defaultString(plugin.Risk, "unknown"))
	}
	return risks
}

func minDiskGB(cfg *config.UserConfig) int {
	if cfg == nil || cfg.Health.MinDiskGB == nil {
		return 80
	}
	return *cfg.Health.MinDiskGB
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func join(values []string) string {
	result := ""
	for i, value := range values {
		if i > 0 {
			result += ", "
		}
		result += value
	}
	return result
}

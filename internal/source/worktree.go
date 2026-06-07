package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/John-Robertt/Auto-OpenWrt/internal/config"
	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

const containerWorktreePath = "/openwrt"

type WorktreePreparer interface {
	PrepareWorktree(context.Context, PrepareWorktreeInput) (*PrepareWorktreeResult, error)
}

type PluginAttacher interface {
	AttachPlugins(context.Context, AttachInput) (*AttachResult, error)
}

type DockerRunner interface {
	Run(ctx context.Context, args ...string) GitResult
}

type PrepareWorktreeInput struct {
	Resolved *config.ResolvedConfig
	RunDir   string
}

type PrepareWorktreeResult struct {
	Manifest     WorktreeManifest
	ManifestPath string
}

type WorktreeManifest struct {
	SchemaVersion       int               `json:"schema_version"`
	WorkspaceID         string            `json:"workspace_id"`
	SourceSetID         string            `json:"source_set_id"`
	BuildID             string            `json:"build_id"`
	RunID               string            `json:"run_id"`
	LogicalWorktreeID   string            `json:"logical_worktree_id"`
	StorageDriver       string            `json:"storage_driver"`
	PhysicalSourcePath  string            `json:"physical_source_path,omitempty"`
	DockerVolumeName    string            `json:"docker_volume_name,omitempty"`
	ContainerPath       string            `json:"container_worktree_path"`
	CaseSensitive       bool              `json:"case_sensitive"`
	FilesystemRisk      string            `json:"filesystem_risk"`
	SourceSetSnapshot   SourceSetSnapshot `json:"source_set_snapshot"`
	AppliedPatchIDs     []string          `json:"applied_adopted_patch_ids"`
	CreatedAt           string            `json:"created_at"`
	StoragePointerPaths map[string]string `json:"storage_pointer_paths,omitempty"`
	DockerHelper        map[string]any    `json:"docker_helper,omitempty"`
}

type AttachInput struct {
	Resolved     *config.ResolvedConfig
	RunDir       string
	Manifest     WorktreeManifest
	ManifestPath string
}

type AttachResult struct {
	SummaryPath string
	Summary     AttachSummary
}

type AttachSummary struct {
	SchemaVersion int           `json:"schema_version"`
	WorkspaceID   string        `json:"workspace_id"`
	SourceSetID   string        `json:"source_set_id"`
	BuildID       string        `json:"build_id"`
	RunID         string        `json:"run_id"`
	Feeds         []AttachEntry `json:"feeds"`
	Plugins       []AttachEntry `json:"plugins"`
	CreatedAt     string        `json:"created_at"`
}

type AttachEntry struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Enabled    bool   `json:"enabled"`
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path"`
	Commit     string `json:"source_commit"`
	Risk       string `json:"risk_type,omitempty"`
	Status     string `json:"attach_status"`
}

type WorktreeError struct {
	Code       string
	Message    string
	Suggestion string
	Details    map[string]any
	Err        error
}

func (e *WorktreeError) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *WorktreeError) Unwrap() error {
	return e.Err
}

type dockerExecRunner struct{}

func (dockerExecRunner) Run(ctx context.Context, args ...string) GitResult {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := GitResult{Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
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

func (m Manager) PrepareWorktree(ctx context.Context, input PrepareWorktreeInput) (*PrepareWorktreeResult, error) {
	if m.Git == nil {
		m.Git = ExecGitRunner{}
	}
	if m.Now == nil {
		m.Now = func() time.Time { return time.Now().UTC() }
	}
	if input.Resolved == nil {
		return nil, worktreeError("WORKTREE_PREPARE_ERROR", "resolved config 为空", "先完成 config.resolve 阶段", nil, nil)
	}
	snapshot, err := readSourceSetSnapshot(m.Store.Root, input.Resolved.SourceSetID)
	if err != nil {
		return nil, worktreeError("SOURCE_SET_SNAPSHOT_MISSING", "source-set snapshot 不可用", "先运行 update 或重新执行 build 以更新源码缓存", map[string]any{"source_set_id": input.Resolved.SourceSetID}, err)
	}

	logicalDir := m.Store.Abs(strings.TrimSuffix(input.Resolved.Workspace.LogicalWorktreeID, "/"))
	if err := os.MkdirAll(logicalDir, 0o755); err != nil {
		return nil, worktreeError("WORKTREE_PREPARE_ERROR", "运行工作树逻辑目录无法创建", "检查 workspace 权限和磁盘空间", map[string]any{"path": logicalDir}, err)
	}

	storage := input.Resolved.Workspace.WorktreeStorage
	physicalPath := ""
	volumeName := ""
	dockerHelper := map[string]any(nil)
	switch storage {
	case "host-path":
		physicalPath = filepath.Join(logicalDir, "source")
		if err := m.cloneOpenWrtWorktree(ctx, snapshot.OpenWrt.CachePath, snapshot.OpenWrt.Commit, physicalPath); err != nil {
			return nil, err
		}
	case "linux-path":
		physicalPath = filepath.Join(input.Resolved.Workspace.LinuxWorktreeRoot, input.Resolved.WorkspaceID, input.Resolved.BuildID, input.Resolved.RunID, "source")
		if err := m.cloneOpenWrtWorktree(ctx, snapshot.OpenWrt.CachePath, snapshot.OpenWrt.Commit, physicalPath); err != nil {
			return nil, err
		}
	case "docker-volume":
		volumeName = dockerVolumeName(input.Resolved)
		if err := writeJSON(filepath.Join(logicalDir, "storage-pointer.json"), map[string]any{
			"schema_version":          SchemaVersion,
			"storage_driver":          "docker-volume",
			"docker_volume_name":      volumeName,
			"container_worktree_path": containerWorktreePath,
		}); err != nil {
			return nil, err
		}
		if err := m.copyToDockerVolume(ctx, input.Resolved, snapshot.OpenWrt.CachePath, volumeName); err != nil {
			return nil, err
		}
		dockerHelper = map[string]any{"image": input.Resolved.Docker.Image, "volume": volumeName}
	default:
		return nil, worktreeError("WORKTREE_STORAGE_UNSUPPORTED", "工作树 storage driver 不受支持", "使用 host-path、linux-path 或 docker-volume", map[string]any{"storage_driver": storage}, nil)
	}

	applied, err := m.applyAdoptedPatches(ctx, input.Resolved, physicalPath, volumeName)
	if err != nil {
		return nil, err
	}
	caseSensitive := true
	if physicalPath != "" {
		caseSensitive = isCaseSensitive(physicalPath)
	}
	manifest := WorktreeManifest{
		SchemaVersion:      SchemaVersion,
		WorkspaceID:        input.Resolved.WorkspaceID,
		SourceSetID:        input.Resolved.SourceSetID,
		BuildID:            input.Resolved.BuildID,
		RunID:              input.Resolved.RunID,
		LogicalWorktreeID:  input.Resolved.Workspace.LogicalWorktreeID,
		StorageDriver:      storage,
		PhysicalSourcePath: physicalPath,
		DockerVolumeName:   volumeName,
		ContainerPath:      containerWorktreePath,
		CaseSensitive:      caseSensitive,
		FilesystemRisk:     filesystemRisk(storage, caseSensitive),
		SourceSetSnapshot:  snapshot,
		AppliedPatchIDs:    applied,
		CreatedAt:          m.Now().UTC().Format(time.RFC3339),
		DockerHelper:       dockerHelper,
	}
	if storage == "docker-volume" {
		manifest.StoragePointerPaths = map[string]string{"logical_pointer": filepath.Join(logicalDir, "storage-pointer.json")}
	}
	manifestPath := filepath.Join(input.RunDir, "worktree-manifest.json")
	if err := writeJSON(manifestPath, manifest); err != nil {
		return nil, err
	}
	return &PrepareWorktreeResult{Manifest: manifest, ManifestPath: manifestPath}, nil
}

func (m Manager) AttachPlugins(ctx context.Context, input AttachInput) (*AttachResult, error) {
	if m.Git == nil {
		m.Git = ExecGitRunner{}
	}
	if m.Now == nil {
		m.Now = func() time.Time { return time.Now().UTC() }
	}
	if input.Resolved == nil {
		return nil, worktreeError("PLUGIN_ATTACH_ERROR", "resolved config 为空", "先完成 config.resolve 阶段", nil, nil)
	}
	if input.Manifest.StorageDriver == "docker-volume" {
		result, err := m.attachPluginsInDockerVolume(ctx, input)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	worktree := input.Manifest.PhysicalSourcePath
	if worktree == "" {
		return nil, worktreeError("PLUGIN_ATTACH_ERROR", "宿主工作树路径为空", "使用 host-path 或 linux-path，或在 Docker 阶段处理 docker-volume 接入", map[string]any{"storage_driver": input.Manifest.StorageDriver}, nil)
	}
	summary := AttachSummary{
		SchemaVersion: SchemaVersion,
		WorkspaceID:   input.Resolved.WorkspaceID,
		SourceSetID:   input.Resolved.SourceSetID,
		BuildID:       input.Resolved.BuildID,
		RunID:         input.Resolved.RunID,
		Feeds:         []AttachEntry{},
		Plugins:       []AttachEntry{},
		CreatedAt:     m.Now().UTC().Format(time.RFC3339),
	}

	feedsConfPath := filepath.Join(worktree, "feeds.conf")
	feedLines := []string{}
	if data, err := os.ReadFile(filepath.Join(worktree, "feeds.conf.default")); err == nil {
		feedLines = append(feedLines, strings.TrimRight(string(data), "\n"))
	}
	for _, name := range input.Resolved.Build.Feeds {
		feed, ok := resolvedFeedByName(input.Resolved.Feeds, name)
		if !ok {
			continue
		}
		sourcePath := filepath.Join(m.Store.Root, "sources", "source-sets", input.Resolved.SourceSetID, "feeds", feed.Name)
		if _, err := os.Stat(sourcePath); err != nil {
			return nil, worktreeError("PLUGIN_ATTACH_MISSING_SOURCE", "feed 源路径不存在", "先运行 update 或检查 feed 配置", map[string]any{"feed": feed.Name, "path": sourcePath}, err)
		}
		containerPath := "/auto-openwrt/sources/" + input.Resolved.SourceSetID + "/feeds/" + feed.Name
		feedLines = upsertFeedLine(feedLines, feed.Name, "src-link "+feed.Name+" "+containerPath)
		summary.Feeds = append(summary.Feeds, AttachEntry{
			Name:       feed.Name,
			Type:       "feed",
			Enabled:    feed.Enabled,
			SourcePath: sourcePath,
			TargetPath: feedsConfPath,
			Commit:     commitForFeed(input.Manifest.SourceSetSnapshot, feed.Name),
			Status:     "attached",
		})
	}
	if err := workspace.AtomicWriteFile(feedsConfPath, []byte(strings.Join(nonEmptyLines(feedLines), "\n")+"\n"), 0o644); err != nil {
		return nil, err
	}

	for _, name := range input.Resolved.Build.Plugins {
		plugin, ok := resolvedPluginByName(input.Resolved.Plugins, name)
		if !ok {
			continue
		}
		entry, err := m.attachPlugin(ctx, input.Resolved, input.Manifest, worktree, feedsConfPath, plugin)
		if err != nil {
			return nil, err
		}
		summary.Plugins = append(summary.Plugins, entry)
	}
	sort.Slice(summary.Feeds, func(i, j int) bool { return summary.Feeds[i].Name < summary.Feeds[j].Name })
	sort.Slice(summary.Plugins, func(i, j int) bool { return summary.Plugins[i].Name < summary.Plugins[j].Name })
	path := filepath.Join(input.RunDir, "plugin-attach-summary.json")
	if err := writeJSON(path, summary); err != nil {
		return nil, err
	}
	return &AttachResult{SummaryPath: path, Summary: summary}, nil
}

func (m Manager) attachPluginsInDockerVolume(ctx context.Context, input AttachInput) (*AttachResult, error) {
	if input.Manifest.DockerVolumeName == "" {
		return nil, worktreeError("PLUGIN_ATTACH_ERROR", "Docker volume 未记录", "重新准备运行工作树", map[string]any{"storage_driver": input.Manifest.StorageDriver}, nil)
	}
	runner := m.Docker
	if runner == nil {
		runner = dockerExecRunner{}
	}
	summary := AttachSummary{
		SchemaVersion: SchemaVersion,
		WorkspaceID:   input.Resolved.WorkspaceID,
		SourceSetID:   input.Resolved.SourceSetID,
		BuildID:       input.Resolved.BuildID,
		RunID:         input.Resolved.RunID,
		Feeds:         []AttachEntry{},
		Plugins:       []AttachEntry{},
		CreatedAt:     m.Now().UTC().Format(time.RFC3339),
	}

	feedLines := []string{}
	if result := runner.Run(ctx, dockerHelperArgs(input.Resolved, []string{"-v", input.Manifest.DockerVolumeName + ":" + containerWorktreePath}, "cat /openwrt/feeds.conf.default 2>/dev/null || true")...); result.Success() {
		feedLines = append(feedLines, strings.TrimRight(result.Stdout, "\n"))
	}
	for _, name := range input.Resolved.Build.Feeds {
		feed, ok := resolvedFeedByName(input.Resolved.Feeds, name)
		if !ok {
			continue
		}
		sourcePath := filepath.Join(m.Store.Root, "sources", "source-sets", input.Resolved.SourceSetID, "feeds", feed.Name)
		if _, err := os.Stat(sourcePath); err != nil {
			return nil, worktreeError("PLUGIN_ATTACH_MISSING_SOURCE", "feed 源路径不存在", "先运行 update 或检查 feed 配置", map[string]any{"feed": feed.Name, "path": sourcePath}, err)
		}
		containerPath := "/auto-openwrt/sources/" + input.Resolved.SourceSetID + "/feeds/" + feed.Name
		feedLines = upsertFeedLine(feedLines, feed.Name, "src-link "+feed.Name+" "+containerPath)
		summary.Feeds = append(summary.Feeds, AttachEntry{
			Name:       feed.Name,
			Type:       "feed",
			Enabled:    feed.Enabled,
			SourcePath: sourcePath,
			TargetPath: "/openwrt/feeds.conf",
			Commit:     commitForFeed(input.Manifest.SourceSetSnapshot, feed.Name),
			Status:     "attached",
		})
	}

	for _, name := range input.Resolved.Build.Plugins {
		plugin, ok := resolvedPluginByName(input.Resolved.Plugins, name)
		if !ok {
			continue
		}
		entry, lines, err := m.attachPluginInDockerVolume(ctx, runner, input.Resolved, input.Manifest, feedLines, plugin)
		if err != nil {
			return nil, err
		}
		feedLines = lines
		summary.Plugins = append(summary.Plugins, entry)
	}
	if err := m.writeDockerVolumeFeedsConf(ctx, runner, input.Resolved, input.Manifest, input.RunDir, feedLines); err != nil {
		return nil, err
	}
	sort.Slice(summary.Feeds, func(i, j int) bool { return summary.Feeds[i].Name < summary.Feeds[j].Name })
	sort.Slice(summary.Plugins, func(i, j int) bool { return summary.Plugins[i].Name < summary.Plugins[j].Name })
	path := filepath.Join(input.RunDir, "plugin-attach-summary.json")
	if err := writeJSON(path, summary); err != nil {
		return nil, err
	}
	return &AttachResult{SummaryPath: path, Summary: summary}, nil
}

func (m Manager) attachPluginInDockerVolume(ctx context.Context, runner DockerRunner, resolved *config.ResolvedConfig, manifest WorktreeManifest, feedLines []string, plugin config.ResolvedPlugin) (AttachEntry, []string, error) {
	root := filepath.Join(m.Store.Root, "sources", "source-sets", resolved.SourceSetID, "plugins", plugin.Name)
	sourcePath := filepath.Join(root, filepath.FromSlash(plugin.Path))
	if plugin.Path == "" {
		sourcePath = root
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return AttachEntry{}, feedLines, worktreeError("PLUGIN_ATTACH_MISSING_SOURCE", "plugin 源路径不存在", "先运行 update 或检查 plugin.path", map[string]any{"plugin": plugin.Name, "path": sourcePath}, err)
	}
	entry := AttachEntry{
		Name:       plugin.Name,
		Type:       plugin.Type,
		Enabled:    plugin.Enabled,
		SourcePath: sourcePath,
		Commit:     commitForPlugin(manifest.SourceSetSnapshot, plugin.Name),
		Risk:       plugin.Risk,
		Status:     "attached",
	}
	switch plugin.Type {
	case "feed":
		containerPath := "/auto-openwrt/sources/" + resolved.SourceSetID + "/plugins/" + plugin.Name
		feedLines = upsertFeedLine(feedLines, plugin.Name, "src-link "+plugin.Name+" "+containerPath)
		entry.TargetPath = "/openwrt/feeds.conf"
	case "patch":
		patches, err := patchFiles(sourcePath)
		if err != nil {
			return AttachEntry{}, feedLines, err
		}
		checkCommands := []string{"cd /openwrt"}
		for _, patch := range patches {
			rel, err := filepath.Rel(sourcePath, patch)
			if err != nil {
				return AttachEntry{}, feedLines, err
			}
			checkCommands = append(checkCommands, "git apply --check "+shellQuote("/auto-openwrt/plugin/"+filepath.ToSlash(rel)))
		}
		for _, patch := range patches {
			rel, err := filepath.Rel(sourcePath, patch)
			if err != nil {
				return AttachEntry{}, feedLines, err
			}
			checkCommands = append(checkCommands, "git apply "+shellQuote("/auto-openwrt/plugin/"+filepath.ToSlash(rel)))
		}
		args := dockerHelperArgs(resolved, []string{"-v", manifest.DockerVolumeName + ":" + containerWorktreePath, "-v", sourcePath + ":/auto-openwrt/plugin:ro"}, strings.Join(checkCommands, " && "))
		if result := runner.Run(ctx, args...); !result.Success() {
			return AttachEntry{}, feedLines, worktreeError("PLUGIN_PATCH_APPLY_FAILED", "docker-volume patch plugin 应用失败", "检查 patch 是否适用于当前 OpenWrt 源码版本，并确认 docker.image 包含 git", map[string]any{"plugin": plugin.Name, "stderr": strings.TrimSpace(result.Stderr)}, result.Err)
		}
		entry.TargetPath = "/openwrt"
	default:
		target := "/openwrt/package/auto-openwrt/" + plugin.Name
		command := "mkdir -p /openwrt/package/auto-openwrt && rm -rf " + shellQuote(target) + " && cp -a /auto-openwrt/plugin/. " + shellQuote(target) + "/"
		args := dockerHelperArgs(resolved, []string{"-v", manifest.DockerVolumeName + ":" + containerWorktreePath, "-v", sourcePath + ":/auto-openwrt/plugin:ro"}, command)
		if result := runner.Run(ctx, args...); !result.Success() {
			return AttachEntry{}, feedLines, worktreeError("PLUGIN_ATTACH_COPY_FAILED", "docker-volume plugin 复制失败", "检查 docker.image 是否具备 sh/cp，并确认 Docker 可访问 plugin source-set cache", map[string]any{"plugin": plugin.Name, "stderr": strings.TrimSpace(result.Stderr)}, result.Err)
		}
		entry.TargetPath = target
	}
	return entry, feedLines, nil
}

func (m Manager) writeDockerVolumeFeedsConf(ctx context.Context, runner DockerRunner, resolved *config.ResolvedConfig, manifest WorktreeManifest, runDir string, feedLines []string) error {
	staging := filepath.Join(runDir, "docker-volume-attach")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	feedsConf := filepath.Join(staging, "feeds.conf")
	if err := workspace.AtomicWriteFile(feedsConf, []byte(strings.Join(nonEmptyLines(feedLines), "\n")+"\n"), 0o644); err != nil {
		return err
	}
	args := dockerHelperArgs(resolved, []string{"-v", manifest.DockerVolumeName + ":" + containerWorktreePath, "-v", staging + ":/auto-openwrt/attach:ro"}, "cp /auto-openwrt/attach/feeds.conf /openwrt/feeds.conf")
	if result := runner.Run(ctx, args...); !result.Success() {
		return worktreeError("PLUGIN_ATTACH_ERROR", "docker-volume feeds.conf 写入失败", "检查 docker.image 是否具备 sh/cp，并确认 Docker volume 可写", map[string]any{"stderr": strings.TrimSpace(result.Stderr)}, result.Err)
	}
	return nil
}

func (m Manager) attachPlugin(ctx context.Context, resolved *config.ResolvedConfig, manifest WorktreeManifest, worktree, feedsConfPath string, plugin config.ResolvedPlugin) (AttachEntry, error) {
	root := filepath.Join(m.Store.Root, "sources", "source-sets", resolved.SourceSetID, "plugins", plugin.Name)
	sourcePath := filepath.Join(root, filepath.FromSlash(plugin.Path))
	if plugin.Path == "" {
		sourcePath = root
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return AttachEntry{}, worktreeError("PLUGIN_ATTACH_MISSING_SOURCE", "plugin 源路径不存在", "先运行 update 或检查 plugin.path", map[string]any{"plugin": plugin.Name, "path": sourcePath}, err)
	}
	entry := AttachEntry{
		Name:       plugin.Name,
		Type:       plugin.Type,
		Enabled:    plugin.Enabled,
		SourcePath: sourcePath,
		Commit:     commitForPlugin(manifest.SourceSetSnapshot, plugin.Name),
		Risk:       plugin.Risk,
		Status:     "attached",
	}
	switch plugin.Type {
	case "feed":
		containerPath := "/auto-openwrt/sources/" + resolved.SourceSetID + "/plugins/" + plugin.Name
		data, err := os.ReadFile(feedsConfPath)
		if err != nil {
			return AttachEntry{}, err
		}
		lines := upsertFeedLine(strings.Split(strings.TrimRight(string(data), "\n"), "\n"), plugin.Name, "src-link "+plugin.Name+" "+containerPath)
		if err := workspace.AtomicWriteFile(feedsConfPath, []byte(strings.Join(nonEmptyLines(lines), "\n")+"\n"), 0o644); err != nil {
			return AttachEntry{}, err
		}
		entry.TargetPath = feedsConfPath
	case "patch":
		patches, err := patchFiles(sourcePath)
		if err != nil {
			return AttachEntry{}, err
		}
		for _, patch := range patches {
			if result := m.Git.Run(ctx, worktree, "apply", "--check", patch); !result.Success() {
				return AttachEntry{}, worktreeError("PLUGIN_PATCH_CHECK_FAILED", "patch plugin 检查失败", "检查 patch 是否适用于当前 OpenWrt 源码版本", map[string]any{"plugin": plugin.Name, "patch": patch, "stderr": strings.TrimSpace(result.Stderr)}, result.Err)
			}
		}
		for _, patch := range patches {
			if result := m.Git.Run(ctx, worktree, "apply", patch); !result.Success() {
				return AttachEntry{}, worktreeError("PLUGIN_PATCH_APPLY_FAILED", "patch plugin 应用失败", "检查 patch 内容后重新运行 build", map[string]any{"plugin": plugin.Name, "patch": patch, "stderr": strings.TrimSpace(result.Stderr)}, result.Err)
			}
		}
		entry.TargetPath = worktree
	default:
		target := filepath.Join(worktree, "package", "auto-openwrt", plugin.Name)
		if err := copyDir(sourcePath, target); err != nil {
			return AttachEntry{}, err
		}
		entry.TargetPath = target
	}
	return entry, nil
}

func (m Manager) cloneOpenWrtWorktree(ctx context.Context, cachePath, commit, target string) error {
	if _, err := os.Stat(target); err == nil {
		return worktreeError("WORKTREE_ALREADY_EXISTS", "运行工作树物理目录已存在", "为新的 run 使用新的 run_id，或清理未完成工作树", map[string]any{"path": target}, nil)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if result := m.Git.Run(ctx, "", "clone", cachePath, target); !result.Success() {
		return worktreeError("WORKTREE_CLONE_FAILED", "运行工作树创建失败", "检查 source-set cache 是否完整并重试 update/build", map[string]any{"cache_path": cachePath, "target": target, "stderr": strings.TrimSpace(result.Stderr)}, result.Err)
	}
	if result := m.Git.Run(ctx, target, "checkout", commit); !result.Success() {
		return worktreeError("WORKTREE_CHECKOUT_FAILED", "运行工作树无法切换到记录 commit", "重新运行 update 后再 build", map[string]any{"commit": commit, "stderr": strings.TrimSpace(result.Stderr)}, result.Err)
	}
	return nil
}

func (m Manager) copyToDockerVolume(ctx context.Context, resolved *config.ResolvedConfig, sourcePath, volumeName string) error {
	runner := m.Docker
	if runner == nil {
		runner = dockerExecRunner{}
	}
	if result := runner.Run(ctx, "volume", "create", volumeName); !result.Success() {
		return worktreeError("DOCKER_VOLUME_PREPARE_FAILED", "Docker volume 创建失败", "检查 Docker daemon 和权限后重试", map[string]any{"volume": volumeName, "stderr": strings.TrimSpace(result.Stderr)}, result.Err)
	}
	command := "cd /openwrt && find . -mindepth 1 -maxdepth 1 -exec rm -rf {} + && cp -a /auto-openwrt/source/. /openwrt/"
	args := dockerHelperArgs(resolved, []string{"-v", sourcePath + ":/auto-openwrt/source:ro", "-v", volumeName + ":" + containerWorktreePath}, command)
	if result := runner.Run(ctx, args...); !result.Success() {
		return worktreeError("DOCKER_VOLUME_PREPARE_FAILED", "Docker volume 工作树复制失败", "检查 docker.image 是否具备 sh/cp 并确认 Docker 可访问源码缓存", map[string]any{"volume": volumeName, "image": resolved.Docker.Image, "stderr": strings.TrimSpace(result.Stderr)}, result.Err)
	}
	return nil
}

func (m Manager) applyAdoptedPatches(ctx context.Context, resolved *config.ResolvedConfig, worktree, volumeName string) ([]string, error) {
	if len(resolved.AdoptedPatchIDs) == 0 {
		return []string{}, nil
	}
	patches := make([]string, 0, len(resolved.AdoptedPatchIDs))
	for _, id := range resolved.AdoptedPatchIDs {
		patchPath := filepath.Join(m.Store.Root, "workspaces", resolved.WorkspaceID, "patches", "adopted", resolved.BuildID, id+".patch")
		if _, err := os.Stat(patchPath); err != nil {
			return nil, worktreeError("ADOPTED_PATCH_MISSING", "adopted patch 文件不存在", "检查 success lock 或移除无效 patch 引用", map[string]any{"patch_id": id, "path": patchPath}, err)
		}
		patches = append(patches, patchPath)
	}
	if worktree == "" && volumeName != "" {
		if err := m.applyAdoptedPatchesInDocker(ctx, resolved, volumeName, patches); err != nil {
			return nil, err
		}
		return append([]string{}, resolved.AdoptedPatchIDs...), nil
	}
	if worktree == "" {
		return nil, worktreeError("ADOPTED_PATCH_APPLY_FAILED", "adopted patch 无法应用", "manifest 缺少可写工作树位置", map[string]any{"storage_driver": resolved.Workspace.WorktreeStorage}, nil)
	}
	for i, patch := range patches {
		if result := m.Git.Run(ctx, worktree, "apply", "--check", patch); !result.Success() {
			return nil, worktreeError("ADOPTED_PATCH_CHECK_FAILED", "adopted patch 检查失败", "检查 patch 是否适用于当前源码版本", map[string]any{"patch_id": resolved.AdoptedPatchIDs[i], "stderr": strings.TrimSpace(result.Stderr)}, result.Err)
		}
	}
	for i, patch := range patches {
		if result := m.Git.Run(ctx, worktree, "apply", patch); !result.Success() {
			return nil, worktreeError("ADOPTED_PATCH_APPLY_FAILED", "adopted patch 应用失败", "检查 patch 内容后重试", map[string]any{"patch_id": resolved.AdoptedPatchIDs[i], "stderr": strings.TrimSpace(result.Stderr)}, result.Err)
		}
	}
	return append([]string{}, resolved.AdoptedPatchIDs...), nil
}

func (m Manager) applyAdoptedPatchesInDocker(ctx context.Context, resolved *config.ResolvedConfig, volumeName string, patches []string) error {
	runner := m.Docker
	if runner == nil {
		runner = dockerExecRunner{}
	}
	patchDir := filepath.Dir(patches[0])
	checkCommands := []string{"cd /openwrt"}
	for _, patch := range patches {
		checkCommands = append(checkCommands, "git apply --check /auto-openwrt/patches/"+filepath.Base(patch))
	}
	for _, patch := range patches {
		checkCommands = append(checkCommands, "git apply /auto-openwrt/patches/"+filepath.Base(patch))
	}
	args := dockerHelperArgs(resolved, []string{"-v", volumeName + ":" + containerWorktreePath, "-v", patchDir + ":/auto-openwrt/patches:ro"}, strings.Join(checkCommands, " && "))
	if result := runner.Run(ctx, args...); !result.Success() {
		return worktreeError("ADOPTED_PATCH_APPLY_FAILED", "docker-volume adopted patch 应用失败", "检查 patch 是否适用于当前源码版本，并确认 docker.image 包含 git", map[string]any{"volume": volumeName, "stderr": strings.TrimSpace(result.Stderr)}, result.Err)
	}
	return nil
}

type successLock struct {
	AdoptedPatchIDs []string `json:"adopted_patch_ids"`
}

func AdoptedPatchIDs(store workspace.Store, workspaceID, buildID string) ([]string, error) {
	path := filepath.Join(store.Root, "workspaces", workspaceID, "locks", buildID, "success-lock.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var lock successLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	return append([]string{}, lock.AdoptedPatchIDs...), nil
}

func readSourceSetSnapshot(root, sourceSetID string) (SourceSetSnapshot, error) {
	path := filepath.Join(root, "sources", "source-sets", sourceSetID, "source-set.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return SourceSetSnapshot{}, err
	}
	var snapshot SourceSetSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return SourceSetSnapshot{}, err
	}
	return snapshot, nil
}

func worktreeError(code, message, suggestion string, details map[string]any, err error) *WorktreeError {
	if details == nil {
		details = map[string]any{}
	}
	return &WorktreeError{Code: code, Message: message, Suggestion: suggestion, Details: details, Err: err}
}

func dockerVolumeName(resolved *config.ResolvedConfig) string {
	name := resolved.Workspace.Name
	if name == "" {
		name = resolved.WorkspaceID
	}
	return fmt.Sprintf("auto-openwrt-%s-%s-%s-%s-worktree", name, resolved.WorkspaceID, resolved.BuildID, resolved.RunID)
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

func filesystemRisk(storage string, caseSensitive bool) string {
	if storage == "host-path" && !caseSensitive {
		return "host path is not case-sensitive"
	}
	return "none"
}

func isCaseSensitive(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	upper := filepath.Join(dir, ".AUTO_OPENWRT_CASE_TEST")
	lower := filepath.Join(dir, ".auto_openwrt_case_test")
	_ = os.Remove(upper)
	_ = os.Remove(lower)
	if err := os.WriteFile(upper, []byte("x"), 0o644); err != nil {
		return false
	}
	defer os.Remove(upper)
	if _, err := os.Stat(lower); err == nil {
		return false
	}
	return true
}

func resolvedFeedByName(feeds []config.ResolvedFeed, name string) (config.ResolvedFeed, bool) {
	for _, feed := range feeds {
		if feed.Name == name {
			return feed, true
		}
	}
	return config.ResolvedFeed{}, false
}

func resolvedPluginByName(plugins []config.ResolvedPlugin, name string) (config.ResolvedPlugin, bool) {
	for _, plugin := range plugins {
		if plugin.Name == name {
			return plugin, true
		}
	}
	return config.ResolvedPlugin{}, false
}

func commitForFeed(snapshot SourceSetSnapshot, name string) string {
	for _, feed := range snapshot.Feeds {
		if feed.Name == name {
			return feed.Commit
		}
	}
	return ""
}

func commitForPlugin(snapshot SourceSetSnapshot, name string) string {
	for _, plugin := range snapshot.Plugins {
		if plugin.Name == name {
			return plugin.Commit
		}
	}
	return ""
}

func upsertFeedLine(lines []string, name, line string) []string {
	prefix := "src-link " + name + " "
	out := []string{}
	replaced := false
	for _, current := range lines {
		if strings.HasPrefix(strings.TrimSpace(current), prefix) {
			if !replaced {
				out = append(out, line)
				replaced = true
			}
			continue
		}
		out = append(out, current)
	}
	if !replaced {
		out = append(out, line)
	}
	return out
}

func nonEmptyLines(lines []string) []string {
	out := []string{}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func patchFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ext := filepath.Ext(entry.Name())
		if ext == ".patch" || ext == ".diff" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst, info.Mode())
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

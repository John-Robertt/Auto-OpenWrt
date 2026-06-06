package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"

	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
	"go.yaml.in/yaml/v3"
)

var runIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-z0-9]{6}$`)

type ResolveInput struct {
	Config      *UserConfig
	ProjectRoot string
	BuildID     string
	RunID       string
	Env         ResolveEnv
}

type ResolveEnv struct {
	GOOS          string
	CaseSensitive bool
	CPUCount      int
}

func DefaultResolveEnv() ResolveEnv {
	return ResolveEnv{
		GOOS:          runtime.GOOS,
		CaseSensitive: runtime.GOOS == "linux",
		CPUCount:      runtime.NumCPU(),
	}
}

func Resolve(input ResolveInput) (*ResolvedConfig, error) {
	if input.Config == nil {
		return nil, newConfigError("CONFIG_SCHEMA_ERROR", "$", "config 不能为空", "先读取 user config")
	}
	if input.ProjectRoot == "" {
		return nil, newConfigError("CONFIG_SCHEMA_ERROR", "project_root", "project root 不能为空", "传入绝对 project root")
	}
	absProject, err := filepath.Abs(input.ProjectRoot)
	if err != nil {
		return nil, newConfigError("CONFIG_SCHEMA_ERROR", "project_root", "project root 无法解析", "检查 project root 路径")
	}
	if input.BuildID == "" {
		return nil, newConfigError("CONFIG_SCHEMA_ERROR", "build_id", "build_id 不能为空", "传入要解析的 build id")
	}
	if input.RunID == "" || !runIDPattern.MatchString(input.RunID) {
		return nil, newConfigError("CONFIG_SCHEMA_ERROR", "run_id", "run_id 格式非法", "使用 YYYYMMDDTHHMMSSZ-<6位小写字母或数字>")
	}

	env := input.Env
	if env.GOOS == "" {
		env = DefaultResolveEnv()
	}
	if env.CPUCount <= 0 {
		env.CPUCount = runtime.NumCPU()
	}

	build, ok := BuildByID(input.Config, input.BuildID)
	if !ok {
		return nil, newConfigError("CONFIG_SCHEMA_ERROR", "build_id", "build 不存在", "选择配置中已声明的 build id")
	}

	storage := resolveWorktreeStorage(input.Config.Workspace, env)
	jobs := resolveJobs(build.Config.Jobs, env.CPUCount)
	workspaceID := input.Config.Workspace.ID
	workspaceName := stringDefault(input.Config.Workspace.Name, workspaceID)
	sourceSetID, err := SourceSetID(input.Config, build)
	if err != nil {
		return nil, err
	}

	return &ResolvedConfig{
		SchemaVersion: 1,
		RunID:         input.RunID,
		WorkspaceID:   workspaceID,
		SourceSetID:   sourceSetID,
		BuildID:       build.ID,
		ProjectRoot:   absProject,
		Workspace: ResolvedWorkspace{
			ID:                workspaceID,
			Name:              workspaceName,
			WorktreeStorage:   storage,
			LinuxWorktreeRoot: input.Config.Workspace.LinuxWorktreeRoot,
			LogicalWorktreeID: fmt.Sprintf("workspaces/%s/worktrees/%s/%s/", workspaceID, build.ID, input.RunID),
		},
		OpenWrt: ResolvedOpenWrt{
			Repo:   input.Config.OpenWrt.Repo,
			Branch: input.Config.OpenWrt.Branch,
			Update: stringDefault(input.Config.OpenWrt.Update, "latest"),
		},
		Docker: ResolvedDocker{
			Image:                 input.Config.Docker.Image,
			Platform:              stringDefault(input.Config.Docker.Platform, "auto"),
			ContainerWorktreePath: "/openwrt",
		},
		Build: ResolvedBuild{
			ID:        build.ID,
			Target:    build.OpenWrt.Target,
			Subtarget: build.OpenWrt.Subtarget,
			Profile:   build.OpenWrt.Profile,
			Feeds:     nonNilStrings(build.Feeds),
			Plugins:   nonNilStrings(build.Plugins),
			Fragments: nonNilStrings(build.Config.Fragments),
			Packages:  nonNilStrings(build.Config.Packages),
			Jobs:      jobs,
		},
		Feeds:           resolveFeeds(input.Config.Feeds),
		Plugins:         resolvePlugins(input.Config.Plugins),
		Health:          ResolvedHealth{MinDiskGB: intPointerDefault(input.Config.Health.MinDiskGB, 80)},
		AIRepair:        resolveAIRepair(input.Config.AIRepair),
		Artifacts:       ResolvedArtifacts{Retention: stringDefault(input.Config.Artifacts.Retention, "keep-all")},
		AdoptedPatchIDs: []string{},
	}, nil
}

func WriteResolvedConfig(store workspace.Store, resolved *ResolvedConfig) (string, error) {
	if resolved == nil {
		return "", newConfigError("CONFIG_SCHEMA_ERROR", "$", "resolved config 不能为空", "先生成 resolved config")
	}
	relPath := filepath.ToSlash(filepath.Join("workspaces", resolved.WorkspaceID, "config", "resolved", resolved.BuildID, resolved.RunID+".yaml"))
	absPath := store.Abs(relPath)
	if _, err := os.Stat(absPath); err == nil {
		return "", newConfigError("RESOLVED_CONFIG_EXISTS", relPath, "resolved config 已存在", "为新的 run 使用新的 run_id")
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	data, err := yaml.Marshal(resolved)
	if err != nil {
		return "", err
	}
	if err := workspace.AtomicWriteFile(absPath, data, 0o644); err != nil {
		return "", err
	}
	return absPath, nil
}

func BuildByID(cfg *UserConfig, id string) (BuildConfig, bool) {
	if cfg == nil {
		return BuildConfig{}, false
	}
	for _, build := range cfg.Builds {
		if build.ID == id {
			return build, true
		}
	}
	return BuildConfig{}, false
}

type sourceSetInput struct {
	OpenWrt sourceSetOpenWrt  `json:"openwrt"`
	Feeds   []sourceSetFeed   `json:"feeds"`
	Plugins []sourceSetPlugin `json:"plugins"`
}

type sourceSetOpenWrt struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Update string `json:"update"`
}

type sourceSetFeed struct {
	Name   string `json:"name"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

type sourceSetPlugin struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

func SourceSetID(cfg *UserConfig, build BuildConfig) (string, error) {
	feedByName := map[string]FeedConfig{}
	for _, feed := range cfg.Feeds {
		feedByName[feed.Name] = feed
	}
	pluginByName := map[string]PluginConfig{}
	for _, plugin := range cfg.Plugins {
		pluginByName[plugin.Name] = plugin
	}

	input := sourceSetInput{
		OpenWrt: sourceSetOpenWrt{
			Repo:   cfg.OpenWrt.Repo,
			Branch: cfg.OpenWrt.Branch,
			Update: stringDefault(cfg.OpenWrt.Update, "latest"),
		},
		Feeds:   []sourceSetFeed{},
		Plugins: []sourceSetPlugin{},
	}
	for _, name := range build.Feeds {
		feed := feedByName[name]
		input.Feeds = append(input.Feeds, sourceSetFeed{Name: feed.Name, Repo: feed.Repo, Branch: feed.Branch, Path: feed.Path})
	}
	for _, name := range build.Plugins {
		plugin := pluginByName[name]
		input.Plugins = append(input.Plugins, sourceSetPlugin{
			Name:   plugin.Name,
			Type:   stringDefault(plugin.Type, "unknown"),
			Repo:   plugin.Repo,
			Branch: plugin.Branch,
			Path:   plugin.Path,
		})
	}
	sort.Slice(input.Feeds, func(i, j int) bool { return input.Feeds[i].Name < input.Feeds[j].Name })
	sort.Slice(input.Plugins, func(i, j int) bool { return input.Plugins[i].Name < input.Plugins[j].Name })

	data, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "src-" + hex.EncodeToString(sum[:])[:12], nil
}

func resolveWorktreeStorage(cfg WorkspaceConfig, env ResolveEnv) string {
	if cfg.WorktreeStorage != "" && cfg.WorktreeStorage != "auto" {
		return cfg.WorktreeStorage
	}
	if cfg.LinuxWorktreeRoot != "" {
		return "linux-path"
	}
	if env.GOOS == "linux" && env.CaseSensitive {
		return "host-path"
	}
	return "docker-volume"
}

func resolveJobs(value any, cpuCount int) int {
	if intValue, ok := value.(int); ok {
		return intValue
	}
	return cpuCount
}

func resolveFeeds(feeds []FeedConfig) []ResolvedFeed {
	resolved := make([]ResolvedFeed, 0, len(feeds))
	for _, feed := range feeds {
		resolved = append(resolved, ResolvedFeed{
			Name:    feed.Name,
			Repo:    feed.Repo,
			Branch:  feed.Branch,
			Path:    feed.Path,
			Enabled: boolDefault(feed.Enabled, true),
		})
	}
	return resolved
}

func resolvePlugins(plugins []PluginConfig) []ResolvedPlugin {
	resolved := make([]ResolvedPlugin, 0, len(plugins))
	for _, plugin := range plugins {
		resolved = append(resolved, ResolvedPlugin{
			Name:    plugin.Name,
			Type:    stringDefault(plugin.Type, "unknown"),
			Repo:    plugin.Repo,
			Branch:  plugin.Branch,
			Path:    plugin.Path,
			Enabled: boolDefault(plugin.Enabled, true),
			Risk:    stringDefault(plugin.Risk, "unknown"),
		})
	}
	return resolved
}

func resolveAIRepair(ai AIRepairConfig) ResolvedAIRepair {
	return ResolvedAIRepair{
		Enabled:    boolDefault(ai.Enabled, false),
		Command:    ai.Command,
		Args:       nonNilStrings(ai.Args),
		Timeout:    stringDefault(ai.Timeout, "30m"),
		MaxRetries: intPointerDefault(ai.MaxRetries, 5),
		Adoption:   stringDefault(ai.Adoption, "auto"),
	}
}

func stringDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

func intPointerDefault(value *int, defaultValue int) int {
	if value == nil {
		return defaultValue
	}
	return *value
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"time"
)

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func validateUserConfig(cfg *UserConfig) error {
	if cfg.Version != 1 {
		return newConfigError("UNSUPPORTED_CONFIG_VERSION", "version", "version 必须为 1", "将 version 设置为 1")
	}
	if cfg.Workspace.ID == "" {
		return newConfigError("CONFIG_SCHEMA_ERROR", "workspace.id", "workspace.id 不能为空", "设置一个用于隔离配置状态的 workspace id")
	}
	if !namePattern.MatchString(cfg.Workspace.ID) {
		return newConfigError("CONFIG_SCHEMA_ERROR", "workspace.id", "workspace.id 格式非法", "使用字母或数字开头，后续只包含字母、数字、点、下划线或连字符")
	}
	if cfg.Workspace.Name != "" && !namePattern.MatchString(cfg.Workspace.Name) {
		return newConfigError("CONFIG_SCHEMA_ERROR", "workspace.name", "workspace.name 格式非法", "使用字母或数字开头，后续只包含字母、数字、点、下划线或连字符")
	}
	if cfg.Workspace.WorktreeStorage != "" && !oneOf(cfg.Workspace.WorktreeStorage, "auto", "host-path", "docker-volume", "linux-path") {
		return newConfigError("CONFIG_SCHEMA_ERROR", "workspace.worktree_storage", "workspace.worktree_storage 不受支持", "使用 auto、host-path、docker-volume 或 linux-path")
	}
	if cfg.Workspace.WorktreeStorage == "linux-path" && !filepath.IsAbs(cfg.Workspace.LinuxWorktreeRoot) {
		return newConfigError("CONFIG_SCHEMA_ERROR", "workspace.linux_worktree_root", "linux-path 需要绝对路径", "为 workspace.linux_worktree_root 设置绝对路径")
	}

	if cfg.OpenWrt.Repo == "" {
		return newConfigError("CONFIG_SCHEMA_ERROR", "openwrt.repo", "openwrt.repo 不能为空", "设置 OpenWrt 源码仓库 URL")
	}
	if cfg.OpenWrt.Branch == "" {
		return newConfigError("CONFIG_SCHEMA_ERROR", "openwrt.branch", "openwrt.branch 不能为空", "设置 OpenWrt 分支")
	}
	if cfg.OpenWrt.Update != "" && cfg.OpenWrt.Update != "latest" {
		return newConfigError("CONFIG_SCHEMA_ERROR", "openwrt.update", "openwrt.update v1 只支持 latest", "将 openwrt.update 设置为 latest")
	}

	if cfg.Docker.Image == "" {
		return newConfigError("CONFIG_SCHEMA_ERROR", "docker.image", "docker.image 不能为空", "设置 Docker 构建镜像")
	}
	if cfg.Docker.Platform == "" {
		// Missing platform is allowed and resolved to auto.
	} else if cfg.Docker.Platform == " " {
		return newConfigError("CONFIG_SCHEMA_ERROR", "docker.platform", "docker.platform 不能为空", "使用 auto 或 Docker 支持的 platform 字符串")
	}

	if len(cfg.Builds) == 0 {
		return newConfigError("CONFIG_SCHEMA_ERROR", "builds", "builds 至少包含一个 build", "添加至少一个 build")
	}
	if err := validateFeeds(cfg.Feeds); err != nil {
		return err
	}
	if err := validatePlugins(cfg.Plugins); err != nil {
		return err
	}
	if err := validateBuilds(cfg); err != nil {
		return err
	}
	if err := validateHealth(cfg.Health); err != nil {
		return err
	}
	if err := validateAIRepair(cfg.AIRepair); err != nil {
		return err
	}
	if cfg.Artifacts.Retention != "" && cfg.Artifacts.Retention != "keep-all" {
		return newConfigError("CONFIG_SCHEMA_ERROR", "artifacts.retention", "artifacts.retention v1 只支持 keep-all", "将 artifacts.retention 设置为 keep-all")
	}
	return nil
}

func validateFeeds(feeds []FeedConfig) error {
	seen := map[string]bool{}
	for i, feed := range feeds {
		path := fmt.Sprintf("feeds[%d]", i)
		if feed.Name == "" {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".name", "feed name 不能为空", "设置唯一 feed 名称")
		}
		if !namePattern.MatchString(feed.Name) {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".name", "feed name 格式非法", "使用字母或数字开头，后续只包含字母、数字、点、下划线或连字符")
		}
		if seen[feed.Name] {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".name", "feed name 重复", "为每个 feed 使用唯一名称")
		}
		seen[feed.Name] = true
		if feed.Repo == "" {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".repo", "feed repo 不能为空", "设置 feed 源码仓库 URL")
		}
		if feed.Branch == "" {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".branch", "feed branch 不能为空", "设置 feed 分支")
		}
		if feed.Path == "" {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".path", "feed path 不能为空", "设置 feed 在 OpenWrt 上下文中的路径标识")
		}
	}
	return nil
}

func validatePlugins(plugins []PluginConfig) error {
	seen := map[string]bool{}
	for i, plugin := range plugins {
		path := fmt.Sprintf("plugins[%d]", i)
		if plugin.Name == "" {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".name", "plugin name 不能为空", "设置唯一 plugin 名称")
		}
		if !namePattern.MatchString(plugin.Name) {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".name", "plugin name 格式非法", "使用字母或数字开头，后续只包含字母、数字、点、下划线或连字符")
		}
		if seen[plugin.Name] {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".name", "plugin name 重复", "为每个 plugin 使用唯一名称")
		}
		seen[plugin.Name] = true
		if plugin.Type != "" && !oneOf(plugin.Type, "feed", "package", "patch", "unknown") {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".type", "plugin type 不受支持", "使用 feed、package、patch 或 unknown")
		}
		if plugin.Repo == "" {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".repo", "plugin repo 不能为空", "设置 plugin 源码仓库 URL")
		}
		if plugin.Branch == "" {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".branch", "plugin branch 不能为空", "设置 plugin 分支")
		}
		if plugin.Risk != "" && !oneOf(plugin.Risk, "luci-app", "kernel-module", "patch", "unknown") {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".risk", "plugin risk 不受支持", "使用 luci-app、kernel-module、patch 或 unknown")
		}
	}
	return nil
}

func validateBuilds(cfg *UserConfig) error {
	feedByName := map[string]FeedConfig{}
	for _, feed := range cfg.Feeds {
		feedByName[feed.Name] = feed
	}
	pluginByName := map[string]PluginConfig{}
	for _, plugin := range cfg.Plugins {
		pluginByName[plugin.Name] = plugin
	}

	seen := map[string]bool{}
	for i, build := range cfg.Builds {
		path := fmt.Sprintf("builds[%d]", i)
		if build.ID == "" {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".id", "build id 不能为空", "设置唯一 build id")
		}
		if !namePattern.MatchString(build.ID) {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".id", "build id 格式非法", "使用字母或数字开头，后续只包含字母、数字、点、下划线或连字符")
		}
		if seen[build.ID] {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".id", "build id 重复", "为每个 build 使用唯一 id")
		}
		seen[build.ID] = true
		if build.OpenWrt.Target == "" {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".openwrt.target", "target 不能为空", "设置 OpenWrt target")
		}
		if build.OpenWrt.Subtarget == "" {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".openwrt.subtarget", "subtarget 不能为空", "设置 OpenWrt subtarget")
		}
		if build.OpenWrt.Profile == "" {
			return newConfigError("CONFIG_SCHEMA_ERROR", path+".openwrt.profile", "device profile 不能为空", "设置 OpenWrt device profile")
		}
		if err := validateJobs(build.Config.Jobs, path+".config.jobs"); err != nil {
			return err
		}
		for j, name := range build.Feeds {
			feed, ok := feedByName[name]
			if !ok {
				return newConfigError("CONFIG_SCHEMA_ERROR", fmt.Sprintf("%s.feeds[%d]", path, j), "build 引用的 feed 不存在", "在顶层 feeds 中声明该 feed 或移除引用")
			}
			if !boolDefault(feed.Enabled, true) {
				return newConfigError("CONFIG_SCHEMA_ERROR", fmt.Sprintf("%s.feeds[%d]", path, j), "build 引用的 feed 已 disabled", "启用该 feed 或从 build 中移除引用")
			}
		}
		for j, name := range build.Plugins {
			plugin, ok := pluginByName[name]
			if !ok {
				return newConfigError("CONFIG_SCHEMA_ERROR", fmt.Sprintf("%s.plugins[%d]", path, j), "build 引用的 plugin 不存在", "在顶层 plugins 中声明该 plugin 或移除引用")
			}
			if !boolDefault(plugin.Enabled, true) {
				return newConfigError("CONFIG_SCHEMA_ERROR", fmt.Sprintf("%s.plugins[%d]", path, j), "build 引用的 plugin 已 disabled", "启用该 plugin 或从 build 中移除引用")
			}
		}
	}
	return nil
}

func validateJobs(value any, path string) error {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case string:
		if typed != "auto" {
			return newConfigError("CONFIG_SCHEMA_ERROR", path, "jobs 字符串值只支持 auto", "使用 jobs: auto 或大于等于 1 的整数")
		}
	case int:
		if typed < 1 {
			return newConfigError("CONFIG_SCHEMA_ERROR", path, "jobs 必须大于等于 1", "使用 jobs: auto 或大于等于 1 的整数")
		}
	default:
		return newConfigError("CONFIG_SCHEMA_ERROR", path, "jobs 类型非法", "使用 jobs: auto 或大于等于 1 的整数")
	}
	return nil
}

func validateHealth(health HealthConfig) error {
	if health.MinDiskGB != nil && *health.MinDiskGB <= 0 {
		return newConfigError("CONFIG_SCHEMA_ERROR", "health.min_disk_gb", "health.min_disk_gb 必须大于 0", "设置正整数磁盘空间阈值")
	}
	return nil
}

func validateAIRepair(ai AIRepairConfig) error {
	if ai.Timeout != "" {
		if _, err := time.ParseDuration(ai.Timeout); err != nil {
			return newConfigError("CONFIG_SCHEMA_ERROR", "ai_repair.timeout", "ai_repair.timeout 无法解析", "使用 Go duration 格式，例如 30m")
		}
	}
	if ai.MaxRetries != nil && (*ai.MaxRetries < 0 || *ai.MaxRetries > 5) {
		return newConfigError("CONFIG_SCHEMA_ERROR", "ai_repair.max_retries", "ai_repair.max_retries 必须在 0..5 范围内", "设置 0 到 5 之间的重试次数")
	}
	if ai.Adoption != "" && ai.Adoption != "auto" {
		return newConfigError("CONFIG_SCHEMA_ERROR", "ai_repair.adoption", "ai_repair.adoption v1 只支持 auto", "将 ai_repair.adoption 设置为 auto")
	}
	if boolDefault(ai.Enabled, false) && ai.Command == "" {
		return newConfigError("CONFIG_SCHEMA_ERROR", "ai_repair.command", "启用 AI 修复时 command 不能为空", "设置可执行的外部 AI CLI 命令")
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func boolDefault(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}

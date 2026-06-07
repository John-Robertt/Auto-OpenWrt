package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

func LoadUserConfig(path string) (*UserConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, newConfigError("CONFIG_READ_ERROR", "$", "配置文件无法读取", "检查 --config 路径和文件权限")
	}
	return parseUserConfig(data, derivedWorkspaceID(path))
}

func LoadUserConfigSnapshot(path string) (*UserConfig, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, newConfigError("CONFIG_READ_ERROR", "$", "配置文件无法读取", "检查 --config 路径和文件权限")
	}
	cfg, err := parseUserConfig(data, derivedWorkspaceID(path))
	if err != nil {
		return nil, nil, err
	}
	return cfg, append([]byte{}, data...), nil
}

func ParseUserConfig(data []byte) (*UserConfig, error) {
	return parseUserConfig(data, "")
}

func parseUserConfig(data []byte, defaultWorkspaceID string) (*UserConfig, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, newConfigError("YAML_PARSE_ERROR", "$", "配置文件无法解析为 YAML", "检查 YAML 语法并重新运行命令")
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil, newConfigError("CONFIG_SCHEMA_ERROR", "$", "配置顶层结构必须是 map", "使用配置规格中的 YAML 示例作为起点")
	}

	root := document.Content[0]
	if mappingValue(root, "profiles") != nil {
		return nil, newConfigError("CONFIG_SCHEMA_ERROR", "profiles", "profiles 已不属于 v1 配置", "将 profiles 迁移为 builds，并使用 builds[].id")
	}
	for _, key := range []string{"version", "workspace", "openwrt", "docker", "builds", "health", "ai_repair", "artifacts"} {
		if mappingValue(root, key) == nil {
			return nil, newConfigError("CONFIG_SCHEMA_ERROR", key, fmt.Sprintf("缺少必填字段 %s", key), "补齐配置规格要求的必填字段")
		}
	}

	var cfg UserConfig
	if err := root.Decode(&cfg); err != nil {
		return nil, newConfigError("CONFIG_SCHEMA_ERROR", "$", "配置字段类型不符合 schema", "检查字段类型是否与配置规格一致")
	}
	if cfg.Feeds == nil {
		cfg.Feeds = []FeedConfig{}
	}
	if cfg.Plugins == nil {
		cfg.Plugins = []PluginConfig{}
	}
	if cfg.Workspace.ID == "" && defaultWorkspaceID != "" {
		cfg.Workspace.ID = defaultWorkspaceID
	}
	if err := validateUserConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func derivedWorkspaceID(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

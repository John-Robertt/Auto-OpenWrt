package app

import (
	"errors"

	"github.com/John-Robertt/Auto-OpenWrt/internal/config"
	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

type InitOptions struct {
	Project string
	Force   bool
}

func Init(options InitOptions) (Result, int) {
	store, err := workspace.New(options.Project)
	if err != nil {
		return Failed("init", "", &Error{
			Code:       "INVALID_PROJECT",
			Message:    "project root 路径无法解析",
			Suggestion: "检查 --project 参数并重新运行 init",
			Details:    map[string]any{"error": err.Error()},
		}), ExitUsageError
	}

	initResult, err := store.Init([]byte(config.SampleYAML), config.SampleWorkspaceID, options.Force)
	if err == nil {
		result := Succeeded("init", initResult.Root, map[string]string{
			"config": initResult.ConfigPath,
		})
		result.WorkspaceID = stringPtr(config.SampleWorkspaceID)
		return result, ExitOK
	}

	var exists *workspace.ConfigExistsError
	if errors.As(err, &exists) {
		return Failed("init", store.Root, &Error{
			Code:       "CONFIG_EXISTS",
			Message:    "配置文件已存在",
			Suggestion: "如需覆盖示例配置，重新运行 init 并传入 --force",
			Details:    map[string]any{"path": exists.Path},
		}), ExitUsageError
	}

	var writeErr *workspace.WriteError
	if errors.As(err, &writeErr) {
		return Failed("init", store.Root, &Error{
			Code:       "WORKSPACE_WRITE_ERROR",
			Message:    "project root 目录或配置文件无法写入",
			Suggestion: "检查 project root 路径、父目录权限和可用磁盘空间",
			Details: map[string]any{
				"path":  writeErr.Path,
				"error": writeErr.Err.Error(),
			},
		}), ExitWorkspaceError
	}

	return Failed("init", store.Root, &Error{
		Code:       "WORKSPACE_WRITE_ERROR",
		Message:    "project root 初始化失败",
		Suggestion: "检查 project root 路径和文件系统权限",
		Details:    map[string]any{"error": err.Error()},
	}), ExitWorkspaceError
}

func stringPtr(value string) *string {
	return &value
}

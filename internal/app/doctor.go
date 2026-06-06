package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/John-Robertt/Auto-OpenWrt/internal/config"
	"github.com/John-Robertt/Auto-OpenWrt/internal/health"
	"github.com/John-Robertt/Auto-OpenWrt/internal/runrecord"
	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

type DoctorOptions struct {
	Project string
	Config  string
	BuildID string
	Checker health.Checker
}

type LogsOptions struct {
	Project string
	Config  string
	BuildID string
	RunID   string
	Latest  bool
}

type bootstrapResult struct {
	Store       workspace.Store
	ConfigPath  string
	Config      *config.UserConfig
	WorkspaceID string
	BuildID     *string
	SourceSetID *string
}

func Doctor(ctx context.Context, options DoctorOptions) (Result, int) {
	store, err := workspace.New(options.Project)
	if err != nil {
		return Failed("doctor", "", invalidProjectError(err)), ExitUsageError
	}
	runStore := runrecord.NewStore(store)
	if _, err := runStore.RecoverIncomplete(runrecord.RecoverOptions{}); err != nil {
		return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	bootstrap, result, code := bootstrapDoctor("doctor", store, options.Config, options.BuildID)
	if code != ExitOK {
		return result, code
	}

	runID, err := runrecord.GenerateRunID(store.Root)
	if err != nil {
		return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	relDir := runrecord.DoctorRunRelDir(&bootstrap.WorkspaceID, runID)
	record, runDir, err := runStore.Create(runrecord.CreateInput{
		Command:     "doctor",
		RunID:       runID,
		ProjectRoot: store.Root,
		WorkspaceID: &bootstrap.WorkspaceID,
		SourceSetID: bootstrap.SourceSetID,
		BuildID:     bootstrap.BuildID,
		RelDir:      relDir,
		Now:         time.Now().UTC(),
	})
	if err != nil {
		return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	if err := runStore.StartStage(runDir, "config.read", "记录 user config 快照", time.Now().UTC()); err != nil {
		return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.FinishStage(runDir, "config.read", runrecord.StatusSucceeded, []string{bootstrap.ConfigPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	var resolved *config.ResolvedConfig
	if bootstrap.BuildID != nil {
		if err := runStore.StartStage(runDir, "config.resolve", "解析 build 和 source set", time.Now().UTC()); err != nil {
			return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
		}
		resolved, err = config.Resolve(config.ResolveInput{
			Config:      bootstrap.Config,
			ProjectRoot: store.Root,
			BuildID:     *bootstrap.BuildID,
			RunID:       runID,
			Env:         config.DefaultResolveEnv(),
		})
		if err != nil {
			errObj := configErrorObject(err)
			_ = runStore.FinishStage(runDir, "config.resolve", runrecord.StatusFailed, nil, errObj, errObj.Suggestion, time.Now().UTC())
			_ = runStore.Complete(runDir, runrecord.FinalBlocked, "config-resolve-failed", errObj, time.Now().UTC())
			return resultFromRun("doctor", store.Root, record, errObj, map[string]string{"run_record": filepath.Join(runDir, "run.json")}, "blocked"), ExitUsageError
		}
		if err := runStore.FinishStage(runDir, "config.resolve", runrecord.StatusSucceeded, nil, nil, "", time.Now().UTC()); err != nil {
			return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
		}
	}

	checker := options.Checker
	if checker == nil {
		checker = health.DefaultChecker{}
	}
	if err := runStore.StartStage(runDir, "health.preflight", "执行预检", time.Now().UTC()); err != nil {
		return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	report, err := checker.Preflight(ctx, health.PreflightInput{
		RunID:       runID,
		ProjectRoot: store.Root,
		ConfigPath:  bootstrap.ConfigPath,
		WorkspaceID: &bootstrap.WorkspaceID,
		SourceSetID: bootstrap.SourceSetID,
		BuildID:     bootstrap.BuildID,
		Config:      bootstrap.Config,
		Resolved:    resolved,
	})
	if err != nil {
		errObj := &runrecord.ErrorObject{Code: "HEALTH_CHECK_ERROR", Message: "健康检查执行失败", Suggestion: "检查运行环境后重新执行 doctor", Details: map[string]any{"error": err.Error()}}
		_ = runStore.FinishStage(runDir, "health.preflight", runrecord.StatusFailed, nil, errObj, errObj.Suggestion, time.Now().UTC())
		_ = runStore.Complete(runDir, runrecord.FinalBlocked, "health-check-error", errObj, time.Now().UTC())
		return resultFromRun("doctor", store.Root, record, errObj, map[string]string{"run_record": filepath.Join(runDir, "run.json")}, "blocked"), ExitWorkspaceError
	}

	healthPath := filepath.Join(runDir, "health-report.json")
	if err := health.WriteReport(healthPath, report); err != nil {
		errObj := &runrecord.ErrorObject{Code: "HEALTH_REPORT_WRITE_ERROR", Message: "Health Report 无法写入", Suggestion: "检查 workspace 权限和磁盘空间", Details: map[string]any{"path": healthPath, "error": err.Error()}}
		_ = runStore.FinishStage(runDir, "health.preflight", runrecord.StatusFailed, nil, errObj, errObj.Suggestion, time.Now().UTC())
		_ = runStore.Complete(runDir, runrecord.FinalBlocked, "health-report-write-failed", errObj, time.Now().UTC())
		return resultFromRun("doctor", store.Root, record, errObj, map[string]string{"run_record": filepath.Join(runDir, "run.json")}, "blocked"), ExitWorkspaceError
	}
	if err := runStore.SetPath(runDir, "health_report", healthPath); err != nil {
		return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	paths := map[string]string{"run_record": filepath.Join(runDir, "run.json"), "health_report": healthPath}
	if !report.CanContinue {
		errObj := &runrecord.ErrorObject{Code: "HEALTH_CHECK_FAILED", Message: "健康检查存在阻断项", Suggestion: "按 Health Report 中 fail 项建议修复后重试", Details: map[string]any{"health_report": healthPath}}
		if err := runStore.FinishStage(runDir, "health.preflight", runrecord.StatusFailed, []string{healthPath}, errObj, errObj.Suggestion, time.Now().UTC()); err != nil {
			return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
		}
		if err := runStore.Complete(runDir, runrecord.FinalBlocked, "health-check-failed", errObj, time.Now().UTC()); err != nil {
			return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
		}
		return resultFromRun("doctor", store.Root, record, errObj, paths, "blocked"), ExitHealthBlocked
	}

	if err := runStore.FinishStage(runDir, "health.preflight", runrecord.StatusSucceeded, []string{healthPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.Complete(runDir, runrecord.FinalSucceeded, "", nil, time.Now().UTC()); err != nil {
		return Failed("doctor", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	result = resultFromRun("doctor", store.Root, record, nil, paths, "succeeded")
	return result, ExitOK
}

func Logs(options LogsOptions) (Result, int) {
	store, err := workspace.New(options.Project)
	if err != nil {
		return Failed("logs", "", invalidProjectError(err)), ExitUsageError
	}

	bootstrap, result, code := bootstrapDoctor("logs", store, options.Config, options.BuildID)
	if code != ExitOK {
		return result, code
	}

	runStore := runrecord.NewStore(store)
	record, path, err := runStore.FindFinal(runrecord.LatestOptions{
		WorkspaceID: &bootstrap.WorkspaceID,
		BuildID:     bootstrap.BuildID,
		RunID:       options.RunID,
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Failed("logs", store.Root, &Error{
				Code:       "RUN_NOT_FOUND",
				Message:    "未找到匹配的 final run",
				Suggestion: "先运行 doctor/build/update，或检查 --run、--build 和 --config 参数",
				Details:    map[string]any{},
			}), ExitUsageError
		}
		return Failed("logs", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	return resultFromRun("logs", store.Root, record, nil, map[string]string{"run_record": path}, "succeeded"), ExitOK
}

func bootstrapDoctor(command string, store workspace.Store, configPathFlag, buildID string) (bootstrapResult, Result, int) {
	configPath := resolveConfigPath(store, configPathFlag)
	cfg, err := config.LoadUserConfig(configPath)
	if err != nil {
		appErr := configAppError(err)
		return bootstrapResult{}, Failed(command, store.Root, appErr), ExitUsageError
	}
	bootstrap := bootstrapResult{
		Store:       store,
		ConfigPath:  configPath,
		Config:      cfg,
		WorkspaceID: cfg.Workspace.ID,
	}
	if buildID == "" {
		return bootstrap, Result{}, ExitOK
	}
	build, ok := config.BuildByID(cfg, buildID)
	if !ok {
		return bootstrapResult{}, Failed(command, store.Root, &Error{
			Code:       "CONFIG_SCHEMA_ERROR",
			Message:    "build 不存在",
			Suggestion: "选择配置中已声明的 build id",
			Details:    map[string]any{"field": "build_id", "build_id": buildID},
		}), ExitUsageError
	}
	sourceSetID, err := config.SourceSetID(cfg, build)
	if err != nil {
		return bootstrapResult{}, Failed(command, store.Root, configAppError(err)), ExitUsageError
	}
	bootstrap.BuildID = stringPtr(build.ID)
	bootstrap.SourceSetID = stringPtr(sourceSetID)
	return bootstrap, Result{}, ExitOK
}

func resolveConfigPath(store workspace.Store, value string) string {
	if value == "" {
		return store.Abs(workspace.ConfigPath)
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return store.Abs(value)
}

func resultFromRun(command, projectRoot string, record runrecord.RunRecord, errObj *runrecord.ErrorObject, paths map[string]string, status string) Result {
	if paths == nil {
		paths = map[string]string{}
	}
	result := Result{
		SchemaVersion: SchemaVersion,
		Command:       command,
		Status:        status,
		ProjectRoot:   projectRoot,
		WorkspaceID:   record.WorkspaceID,
		SourceSetID:   record.SourceSetID,
		BuildID:       record.BuildID,
		RunID:         &record.RunID,
		Paths:         paths,
		Error:         nil,
	}
	if errObj != nil {
		result.Error = &Error{
			Code:       errObj.Code,
			Message:    errObj.Message,
			Suggestion: errObj.Suggestion,
			Details:    errObj.Details,
		}
	}
	return result
}

func configAppError(err error) *Error {
	var cfgErr *config.ConfigError
	if errors.As(err, &cfgErr) {
		return &Error{
			Code:       cfgErr.Code,
			Message:    cfgErr.Message,
			Suggestion: cfgErr.Suggestion,
			Details:    cfgErr.Details,
		}
	}
	return &Error{
		Code:       "CONFIG_ERROR",
		Message:    "配置处理失败",
		Suggestion: "检查配置文件后重试",
		Details:    map[string]any{"error": err.Error()},
	}
}

func configErrorObject(err error) *runrecord.ErrorObject {
	appErr := configAppError(err)
	return &runrecord.ErrorObject{
		Code:       appErr.Code,
		Message:    appErr.Message,
		Suggestion: appErr.Suggestion,
		Details:    appErr.Details,
	}
}

func invalidProjectError(err error) *Error {
	return &Error{
		Code:       "INVALID_PROJECT",
		Message:    "project root 路径无法解析",
		Suggestion: "检查 --project 参数并重新运行命令",
		Details:    map[string]any{"error": err.Error()},
	}
}

func workspaceWriteError(err error) *Error {
	return &Error{
		Code:       "WORKSPACE_WRITE_ERROR",
		Message:    "workspace 状态无法写入或读取",
		Suggestion: "检查 project root、workspace 权限和可用磁盘空间",
		Details:    map[string]any{"error": err.Error()},
	}
}

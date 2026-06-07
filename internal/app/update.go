package app

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/John-Robertt/Auto-OpenWrt/internal/config"
	"github.com/John-Robertt/Auto-OpenWrt/internal/runrecord"
	"github.com/John-Robertt/Auto-OpenWrt/internal/source"
	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

type UpdateOptions struct {
	Project string
	Config  string
	BuildID string
	Updater SourceUpdater
}

type SourceUpdater interface {
	Update(context.Context, source.UpdateInput) (*source.UpdateResult, error)
}

type updateBootstrap struct {
	ConfigPath  string
	RawConfig   []byte
	Config      *config.UserConfig
	WorkspaceID string
	BuildID     *string
	SourceSetID *string
	Plans       []config.SourceSetPlan
}

func Update(ctx context.Context, options UpdateOptions) (Result, int) {
	store, err := workspace.New(options.Project)
	if err != nil {
		return Failed("update", "", invalidProjectError(err)), ExitUsageError
	}
	runStore := runrecord.NewStore(store)
	if _, err := runStore.RecoverIncomplete(runrecord.RecoverOptions{}); err != nil {
		return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	bootstrap, result, code := bootstrapUpdate(store, options.Config, options.BuildID)
	if code != ExitOK {
		return result, code
	}

	runID, err := runrecord.GenerateRunID(store.Root)
	if err != nil {
		return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	record, runDir, err := runStore.Create(runrecord.CreateInput{
		Command:     "update",
		RunID:       runID,
		ProjectRoot: store.Root,
		WorkspaceID: &bootstrap.WorkspaceID,
		SourceSetID: bootstrap.SourceSetID,
		BuildID:     bootstrap.BuildID,
		RelDir:      runrecord.UpdateRunRelDir(bootstrap.WorkspaceID, runID),
		Now:         time.Now().UTC(),
	})
	if err != nil {
		return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	if err := runStore.StartStage(runDir, "config.read", "记录 user config 快照", time.Now().UTC()); err != nil {
		return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	configSnapshotPath, err := writeConfigSnapshot(runDir, bootstrap.RawConfig)
	if err != nil {
		errObj := workspaceErrorObject("CONFIG_SNAPSHOT_WRITE_ERROR", "user config 快照无法写入", "检查 run record 目录权限和磁盘空间", err)
		_ = runStore.FinishStage(runDir, "config.read", runrecord.StatusFailed, nil, errObj, errObj.Suggestion, time.Now().UTC())
		_ = runStore.Complete(runDir, runrecord.FinalBlocked, "config-snapshot-write-failed", errObj, time.Now().UTC())
		return resultFromRun("update", store.Root, record, errObj, map[string]string{"run_record": filepath.Join(runDir, "run.json")}, "blocked"), ExitWorkspaceError
	}
	if err := runStore.SetPath(runDir, "user_config", configSnapshotPath); err != nil {
		errObj := workspaceErrorObject("CONFIG_SNAPSHOT_WRITE_ERROR", "user config 快照路径无法写入 run record", "检查 run record 目录权限和磁盘空间", err)
		_ = runStore.FinishStage(runDir, "config.read", runrecord.StatusFailed, []string{configSnapshotPath}, errObj, errObj.Suggestion, time.Now().UTC())
		_ = runStore.Complete(runDir, runrecord.FinalBlocked, "config-snapshot-path-write-failed", errObj, time.Now().UTC())
		return resultFromRun("update", store.Root, record, errObj, map[string]string{"run_record": filepath.Join(runDir, "run.json"), "user_config": configSnapshotPath}, "blocked"), ExitWorkspaceError
	}
	if err := runStore.FinishStage(runDir, "config.read", runrecord.StatusSucceeded, []string{configSnapshotPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.StartStage(runDir, "config.resolve", "解析 update source-set 集合", time.Now().UTC()); err != nil {
		return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.FinishStage(runDir, "config.resolve", runrecord.StatusSucceeded, nil, nil, "", time.Now().UTC()); err != nil {
		return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	updater := options.Updater
	if updater == nil {
		updater = source.Manager{Store: store}
	}
	if err := runStore.StartStage(runDir, "source.update", "更新 source-set 源码缓存", time.Now().UTC()); err != nil {
		return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	updateResult, err := updater.Update(ctx, source.UpdateInput{
		WorkspaceID: bootstrap.WorkspaceID,
		BuildID:     bootstrap.BuildID,
		RunID:       runID,
		RunDir:      runDir,
		Plans:       bootstrap.Plans,
	})
	if err != nil {
		errObj := sourceErrorObject(err)
		_ = runStore.FinishStage(runDir, "source.update", runrecord.StatusFailed, nil, errObj, errObj.Suggestion, time.Now().UTC())
		_ = runStore.Complete(runDir, runrecord.FinalFailed, "source-update-failed", errObj, time.Now().UTC())
		return resultFromRun("update", store.Root, record, errObj, map[string]string{"run_record": filepath.Join(runDir, "run.json")}, "failed"), ExitSourceError
	}
	if err := runStore.SetPath(runDir, "source_update_summary", updateResult.SummaryPath); err != nil {
		return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if len(updateResult.Snapshots) == 1 {
		snapshotPath := filepath.Join(store.Root, "sources", "source-sets", updateResult.Snapshots[0].SourceSetID, "source-set.json")
		if err := runStore.SetPath(runDir, "source_set_snapshot", snapshotPath); err != nil {
			return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
		}
	}
	if err := runStore.SetPath(runDir, "source_set_ids", joinIDs(updateResult.SourceSetIDs)); err != nil {
		return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	paths := map[string]string{
		"run_record":            filepath.Join(runDir, "run.json"),
		"user_config":           configSnapshotPath,
		"source_update_summary": updateResult.SummaryPath,
	}
	if len(updateResult.Snapshots) == 1 {
		paths["source_set_snapshot"] = filepath.Join(store.Root, "sources", "source-sets", updateResult.Snapshots[0].SourceSetID, "source-set.json")
	}
	if err := runStore.FinishStage(runDir, "source.update", runrecord.StatusSucceeded, []string{updateResult.SummaryPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.Complete(runDir, runrecord.FinalSucceeded, "", nil, time.Now().UTC()); err != nil {
		return Failed("update", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	return resultFromRun("update", store.Root, record, nil, paths, "succeeded"), ExitOK
}

func bootstrapUpdate(store workspace.Store, configPathFlag, buildID string) (updateBootstrap, Result, int) {
	configPath := resolveConfigPath(store, configPathFlag)
	cfg, raw, err := config.LoadUserConfigSnapshot(configPath)
	if err != nil {
		return updateBootstrap{}, Failed("update", store.Root, configAppError(err)), ExitUsageError
	}
	plans, err := config.UpdateSourceSetPlans(cfg, buildID)
	if err != nil {
		return updateBootstrap{}, Failed("update", store.Root, configAppError(err)), ExitUsageError
	}
	bootstrap := updateBootstrap{
		ConfigPath:  configPath,
		RawConfig:   raw,
		Config:      cfg,
		WorkspaceID: cfg.Workspace.ID,
		Plans:       plans,
	}
	if buildID != "" {
		bootstrap.BuildID = stringPtr(buildID)
	}
	if len(plans) == 1 && len(plans[0].BuildIDs) == 1 {
		bootstrap.SourceSetID = stringPtr(plans[0].SourceSetID)
		if bootstrap.BuildID == nil {
			bootstrap.BuildID = stringPtr(plans[0].BuildIDs[0])
		}
	}
	return bootstrap, Result{}, ExitOK
}

func sourceErrorObject(err error) *runrecord.ErrorObject {
	var repoErr *source.RepositoryError
	if errors.As(err, &repoErr) {
		return &runrecord.ErrorObject{
			Code:       "SOURCE_UPDATE_ERROR",
			Message:    "源码缓存更新失败",
			Suggestion: "检查仓库 URL、分支、网络和本地 Git 权限后重新运行 update",
			Details: map[string]any{
				"repository": repoErr.Name,
				"repo":       repoErr.Repo,
				"command":    repoErr.Command,
				"exit_code":  repoErr.ExitCode,
				"stderr":     repoErr.Stderr,
			},
		}
	}
	return &runrecord.ErrorObject{
		Code:       "SOURCE_UPDATE_ERROR",
		Message:    "源码缓存更新失败",
		Suggestion: "检查 Git、仓库配置和 project root 权限后重新运行 update",
		Details:    map[string]any{"error": err.Error()},
	}
}

func joinIDs(values []string) string {
	if len(values) == 0 {
		return ""
	}
	result := values[0]
	for _, value := range values[1:] {
		result += "," + value
	}
	return result
}

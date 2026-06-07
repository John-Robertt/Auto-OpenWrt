package app

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/John-Robertt/Auto-OpenWrt/internal/artifact"
	"github.com/John-Robertt/Auto-OpenWrt/internal/buildconfig"
	"github.com/John-Robertt/Auto-OpenWrt/internal/config"
	"github.com/John-Robertt/Auto-OpenWrt/internal/dockerexec"
	"github.com/John-Robertt/Auto-OpenWrt/internal/health"
	"github.com/John-Robertt/Auto-OpenWrt/internal/runrecord"
	"github.com/John-Robertt/Auto-OpenWrt/internal/source"
	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

type BuildOptions struct {
	Project          string
	Config           string
	BuildID          string
	Checker          health.Checker
	Updater          SourceUpdater
	WorktreePreparer source.WorktreePreparer
	PluginAttacher   source.PluginAttacher
	ConfigGenerator  buildconfig.Generator
	DockerExecutor   dockerexec.Executor
	ArtifactRecorder artifact.Recorder
}

func Build(ctx context.Context, options BuildOptions) (Result, int) {
	if options.BuildID == "" {
		return Failed("build", "", &Error{
			Code:       "INVALID_ARGUMENT",
			Message:    "--build 为必填参数",
			Suggestion: "传入 --build <id>",
			Details:    map[string]any{},
		}), ExitUsageError
	}
	store, err := workspace.New(options.Project)
	if err != nil {
		return Failed("build", "", invalidProjectError(err)), ExitUsageError
	}
	runStore := runrecord.NewStore(store)
	if _, err := runStore.RecoverIncomplete(runrecord.RecoverOptions{}); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	bootstrap, result, code := bootstrapDoctor("build", store, options.Config, options.BuildID)
	if code != ExitOK {
		return result, code
	}

	runID, err := runrecord.GenerateRunID(store.Root)
	if err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	record, runDir, err := runStore.Create(runrecord.CreateInput{
		Command:     "build",
		RunID:       runID,
		ProjectRoot: store.Root,
		WorkspaceID: &bootstrap.WorkspaceID,
		SourceSetID: bootstrap.SourceSetID,
		BuildID:     bootstrap.BuildID,
		RelDir:      runrecord.BuildRunRelDir(bootstrap.WorkspaceID, *bootstrap.BuildID, runID),
		Now:         time.Now().UTC(),
	})
	if err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	paths := map[string]string{"run_record": filepath.Join(runDir, "run.json")}

	if err := runStore.StartStage(runDir, "config.read", "记录 user config 快照", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	configSnapshotPath, err := writeConfigSnapshot(runDir, bootstrap.RawConfig)
	if err != nil {
		errObj := workspaceErrorObject("CONFIG_SNAPSHOT_WRITE_ERROR", "user config 快照无法写入", "检查 run record 目录权限和磁盘空间", err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "config.read", "config-snapshot-write-failed", errObj, paths, ExitWorkspaceError)
	}
	paths["user_config"] = configSnapshotPath
	if err := runStore.SetPath(runDir, "user_config", configSnapshotPath); err != nil {
		errObj := workspaceErrorObject("CONFIG_SNAPSHOT_WRITE_ERROR", "user config 快照路径无法写入 run record", "检查 run record 目录权限和磁盘空间", err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "config.read", "config-snapshot-path-write-failed", errObj, paths, ExitWorkspaceError)
	}
	if err := runStore.FinishStage(runDir, "config.read", runrecord.StatusSucceeded, []string{configSnapshotPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	if err := runStore.StartStage(runDir, "config.resolve", "解析 build 和 source set", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	adoptedIDs, err := source.AdoptedPatchIDs(store, bootstrap.WorkspaceID, *bootstrap.BuildID)
	if err != nil {
		errObj := workspaceErrorObject("SUCCESS_LOCK_READ_ERROR", "success lock 无法读取", "检查 success lock JSON 或移除损坏文件", err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "config.resolve", "success-lock-read-failed", errObj, paths, ExitWorkspaceError)
	}
	resolved, err := config.Resolve(config.ResolveInput{
		Config:          bootstrap.Config,
		ProjectRoot:     store.Root,
		BuildID:         *bootstrap.BuildID,
		RunID:           runID,
		AdoptedPatchIDs: adoptedIDs,
		Env:             config.ProjectResolveEnv(store.Root),
	})
	if err != nil {
		errObj := configErrorObject(err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "config.resolve", "config-resolve-failed", errObj, paths, ExitUsageError)
	}
	resolvedPath, err := config.WriteResolvedConfig(store, resolved)
	if err != nil {
		errObj := workspaceErrorObject("RESOLVED_CONFIG_WRITE_ERROR", "resolved config 无法写入", "检查 workspace 权限和磁盘空间", err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "config.resolve", "resolved-config-write-failed", errObj, paths, ExitWorkspaceError)
	}
	paths["resolved_config"] = resolvedPath
	if err := runStore.SetPath(runDir, "resolved_config", resolvedPath); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.FinishStage(runDir, "config.resolve", runrecord.StatusSucceeded, []string{resolvedPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	checker := options.Checker
	if checker == nil {
		checker = health.DefaultChecker{}
	}
	if err := runStore.StartStage(runDir, "health.preflight", "执行预检", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
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
		errObj := workspaceErrorObject("HEALTH_CHECK_ERROR", "健康检查执行失败", "检查运行环境后重新执行 build", err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "health.preflight", "health-check-error", errObj, paths, ExitWorkspaceError)
	}
	healthPath := filepath.Join(runDir, "health-report.json")
	if err := health.WriteReport(healthPath, report); err != nil {
		errObj := workspaceErrorObject("HEALTH_REPORT_WRITE_ERROR", "Health Report 无法写入", "检查 workspace 权限和磁盘空间", err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "health.preflight", "health-report-write-failed", errObj, paths, ExitWorkspaceError)
	}
	paths["health_report"] = healthPath
	if err := runStore.SetPath(runDir, "health_report", healthPath); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if !report.CanContinue {
		errObj := &runrecord.ErrorObject{Code: "HEALTH_CHECK_FAILED", Message: "健康检查存在阻断项", Suggestion: "按 Health Report 中 fail 项建议修复后重试", Details: map[string]any{"health_report": healthPath}}
		return finishBlockedWithResults("build", store.Root, record, runStore, runDir, "health.preflight", "health-check-failed", errObj, paths, []string{healthPath}, ExitHealthBlocked)
	}
	if err := runStore.FinishStage(runDir, "health.preflight", runrecord.StatusSucceeded, []string{healthPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	updater := options.Updater
	if updater == nil {
		updater = source.Manager{Store: store}
	}
	if err := runStore.StartStage(runDir, "source.update", "更新 source-set 源码缓存", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	updatePlans, err := config.UpdateSourceSetPlans(bootstrap.Config, *bootstrap.BuildID)
	if err != nil {
		errObj := configErrorObject(err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "source.update", "source-plan-failed", errObj, paths, ExitUsageError)
	}
	updateResult, err := updater.Update(ctx, source.UpdateInput{
		WorkspaceID: bootstrap.WorkspaceID,
		BuildID:     bootstrap.BuildID,
		RunID:       runID,
		RunDir:      runDir,
		Plans:       updatePlans,
	})
	if err != nil {
		errObj := sourceErrorObject(err)
		return finishFailed("build", store.Root, record, runStore, runDir, "source.update", "source-update-failed", errObj, paths, ExitSourceError)
	}
	paths["source_update_summary"] = updateResult.SummaryPath
	if err := runStore.SetPath(runDir, "source_update_summary", updateResult.SummaryPath); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if len(updateResult.Snapshots) == 1 {
		snapshotPath := filepath.Join(store.Root, "sources", "source-sets", updateResult.Snapshots[0].SourceSetID, "source-set.json")
		paths["source_set_snapshot"] = snapshotPath
		if err := runStore.SetPath(runDir, "source_set_snapshot", snapshotPath); err != nil {
			return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
		}
	}
	if err := runStore.FinishStage(runDir, "source.update", runrecord.StatusSucceeded, []string{updateResult.SummaryPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	preparer := options.WorktreePreparer
	if preparer == nil {
		preparer = source.Manager{Store: store}
	}
	if err := runStore.StartStage(runDir, "worktree.prepare", "准备运行工作树并应用 adopted patches", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	worktreeResult, err := preparer.PrepareWorktree(ctx, source.PrepareWorktreeInput{Resolved: resolved, RunDir: runDir})
	if err != nil {
		errObj := worktreeErrorObject(err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "worktree.prepare", "worktree-prepare-failed", errObj, paths, ExitSourceError)
	}
	paths["worktree_manifest"] = worktreeResult.ManifestPath
	if err := runStore.SetPath(runDir, "worktree_manifest", worktreeResult.ManifestPath); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.FinishStage(runDir, "worktree.prepare", runrecord.StatusSucceeded, []string{worktreeResult.ManifestPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	attacher := options.PluginAttacher
	if attacher == nil {
		attacher = source.Manager{Store: store}
	}
	if err := runStore.StartStage(runDir, "plugins.attach", "接入 feeds 和 plugins", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	attachResult, err := attacher.AttachPlugins(ctx, source.AttachInput{Resolved: resolved, RunDir: runDir, Manifest: worktreeResult.Manifest, ManifestPath: worktreeResult.ManifestPath})
	if err != nil {
		errObj := worktreeErrorObject(err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "plugins.attach", "plugins-attach-failed", errObj, paths, ExitSourceError)
	}
	paths["plugin_attach_summary"] = attachResult.SummaryPath
	if err := runStore.SetPath(runDir, "plugin_attach_summary", attachResult.SummaryPath); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.FinishStage(runDir, "plugins.attach", runrecord.StatusSucceeded, []string{attachResult.SummaryPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	if err := runStore.StartStage(runDir, "health.build_context", "执行构建上下文校验", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	report, err = checker.BuildContext(ctx, health.BuildContextInput{
		RunID:         runID,
		ProjectRoot:   store.Root,
		WorkspaceID:   bootstrap.WorkspaceID,
		SourceSetID:   resolved.SourceSetID,
		BuildID:       resolved.BuildID,
		Resolved:      resolved,
		Manifest:      worktreeResult.Manifest,
		ManifestPath:  worktreeResult.ManifestPath,
		AttachSummary: &attachResult.Summary,
		Existing:      report,
	})
	if err != nil {
		errObj := workspaceErrorObject("HEALTH_CHECK_ERROR", "构建上下文校验执行失败", "检查运行工作树和插件材料后重试", err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "health.build_context", "build-context-error", errObj, paths, ExitWorkspaceError)
	}
	if err := health.WriteReport(healthPath, report); err != nil {
		errObj := workspaceErrorObject("HEALTH_REPORT_WRITE_ERROR", "Health Report 无法写入", "检查 workspace 权限和磁盘空间", err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "health.build_context", "health-report-write-failed", errObj, paths, ExitWorkspaceError)
	}
	if !report.CanContinue {
		errObj := &runrecord.ErrorObject{Code: "HEALTH_CHECK_FAILED", Message: "构建上下文校验存在阻断项", Suggestion: "按 Health Report 中 fail 项建议修复后重试", Details: map[string]any{"health_report": healthPath}}
		return finishBlockedWithResults("build", store.Root, record, runStore, runDir, "health.build_context", "build-context-failed", errObj, paths, []string{healthPath}, ExitHealthBlocked)
	}
	if err := runStore.FinishStage(runDir, "health.build_context", runrecord.StatusSucceeded, []string{healthPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	configGenerator := options.ConfigGenerator
	if configGenerator == nil {
		configGenerator = buildconfig.DefaultGenerator{}
	}
	if err := runStore.StartStage(runDir, "build.config", "生成 OpenWrt 构建配置", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	buildConfigResult, err := configGenerator.Generate(ctx, buildconfig.Input{
		ProjectRoot: store.Root,
		Resolved:    resolved,
		Manifest:    worktreeResult.Manifest,
		RunDir:      runDir,
	})
	if err != nil {
		errObj := buildConfigErrorObject(err)
		return finishBlocked("build", store.Root, record, runStore, runDir, "build.config", "build-config-failed", errObj, paths, ExitWorkspaceError)
	}
	paths["build_config_summary"] = buildConfigResult.SummaryPath
	paths["build_config_fragments"] = buildConfigResult.FragmentDir
	if err := runStore.SetPath(runDir, "build_config_summary", buildConfigResult.SummaryPath); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.SetPath(runDir, "build_config_fragments", buildConfigResult.FragmentDir); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.FinishStage(runDir, "build.config", runrecord.StatusSucceeded, []string{buildConfigResult.SummaryPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	artifactStaging := filepath.Join(store.Root, "workspaces", resolved.WorkspaceID, "artifacts", ".staging", resolved.BuildID, resolved.RunID)
	artifactFinal := filepath.Join(store.Root, "workspaces", resolved.WorkspaceID, "artifacts", resolved.BuildID, resolved.RunID)
	successLockPath := filepath.Join(store.Root, "workspaces", resolved.WorkspaceID, "locks", resolved.BuildID, "success-lock.json")
	dockerLogPath := filepath.Join(runDir, "logs", "docker-build.log")
	dockerSummaryPath := filepath.Join(runDir, "docker-env-summary.json")
	paths["docker_build_log"] = dockerLogPath
	paths["docker_env_summary"] = dockerSummaryPath
	if err := runStore.SetPath(runDir, "docker_build_log", dockerLogPath); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.SetPath(runDir, "docker_env_summary", dockerSummaryPath); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}

	dockerExecutor := options.DockerExecutor
	if dockerExecutor == nil {
		dockerExecutor = dockerexec.DefaultExecutor{}
	}
	if err := runStore.StartStage(runDir, "docker.build", "执行 Docker 构建", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	dockerResult, err := dockerExecutor.Build(ctx, dockerexec.Input{
		ProjectRoot:     store.Root,
		Resolved:        resolved,
		Manifest:        worktreeResult.Manifest,
		AttachSummary:   attachResult.Summary,
		DownloadCache:   filepath.Join(store.Root, "cache", "downloads"),
		BuildCache:      filepath.Join(store.Root, "cache", "build"),
		ArtifactStaging: artifactStaging,
		LogPath:         dockerLogPath,
		SummaryPath:     dockerSummaryPath,
	})
	if dockerResult != nil {
		paths["docker_build_log"] = dockerResult.LogPath
		paths["docker_env_summary"] = dockerResult.SummaryPath
		_ = runStore.SetPath(runDir, "docker_build_log", dockerResult.LogPath)
		_ = runStore.SetPath(runDir, "docker_env_summary", dockerResult.SummaryPath)
	}
	recorder := options.ArtifactRecorder
	if recorder == nil {
		recorder = artifact.DefaultRecorder{}
	}
	if err != nil {
		errObj := dockerErrorObject(err)
		_ = runStore.FinishStage(runDir, "docker.build", runrecord.StatusFailed, []string{dockerLogPath, dockerSummaryPath}, errObj, errObj.Suggestion, time.Now().UTC())
		exitCode := dockerExitCode(err)
		return archiveBuildFailure(ctx, archiveFailureInput{
			command:                 "build",
			root:                    store.Root,
			record:                  record,
			runStore:                runStore,
			runDir:                  runDir,
			resolved:                resolved,
			manifest:                worktreeResult.Manifest,
			manifestPath:            worktreeResult.ManifestPath,
			healthPath:              healthPath,
			resolvedPath:            resolvedPath,
			sourceUpdateSummaryPath: updateResult.SummaryPath,
			dockerLogPath:           dockerLogPath,
			dockerSummaryPath:       dockerSummaryPath,
			failureStage:            "docker.build",
			failureTarget:           resolved.Build.Target + "/" + resolved.Build.Subtarget,
			errObj:                  errObj,
			paths:                   paths,
			recorder:                recorder,
			exitCode:                exitCode,
		})
	}
	if err := runStore.FinishStage(runDir, "docker.build", runrecord.StatusSucceeded, []string{dockerLogPath, dockerSummaryPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.StartStage(runDir, "build.result", "判定构建结果", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.FinishStage(runDir, "build.result", runrecord.StatusSucceeded, []string{dockerLogPath, dockerSummaryPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.StartStage(runDir, "artifact.archive", "归档成功产物并写入 success lock", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	archiveResult, err := recorder.ArchiveSuccess(ctx, artifact.SuccessInput{
		ProjectRoot:             store.Root,
		Resolved:                resolved,
		Manifest:                worktreeResult.Manifest,
		ManifestPath:            worktreeResult.ManifestPath,
		HealthReportPath:        healthPath,
		ResolvedConfigPath:      resolvedPath,
		SourceUpdateSummaryPath: updateResult.SummaryPath,
		DockerLogPath:           dockerLogPath,
		DockerSummaryPath:       dockerSummaryPath,
		RunDir:                  runDir,
		ArtifactStaging:         artifactStaging,
		ArtifactFinal:           artifactFinal,
		SuccessLockPath:         successLockPath,
	})
	if err != nil {
		errObj := artifactErrorObject(err)
		return finishBlockedWithResults("build", store.Root, record, runStore, runDir, "artifact.archive", "artifact-archive-failed", errObj, paths, nil, ExitWorkspaceError)
	}
	paths["artifact_index"] = archiveResult.ArtifactIndexPath
	paths["artifact_dir"] = archiveResult.ArtifactDir
	paths["success_lock"] = archiveResult.SuccessLockPath
	if err := runStore.SetPath(runDir, "artifact_index", archiveResult.ArtifactIndexPath); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.SetPath(runDir, "artifact_dir", archiveResult.ArtifactDir); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.SetPath(runDir, "success_lock", archiveResult.SuccessLockPath); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.FinishStage(runDir, "artifact.archive", runrecord.StatusSucceeded, []string{archiveResult.ArtifactIndexPath, archiveResult.SuccessLockPath}, nil, "", time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	if err := runStore.Complete(runDir, runrecord.FinalSucceeded, "", nil, time.Now().UTC()); err != nil {
		return Failed("build", store.Root, workspaceWriteError(err)), ExitWorkspaceError
	}
	return resultFromRun("build", store.Root, record, nil, paths, "succeeded"), ExitOK
}

type archiveFailureInput struct {
	command                 string
	root                    string
	record                  runrecord.RunRecord
	runStore                runrecord.Store
	runDir                  string
	resolved                *config.ResolvedConfig
	manifest                source.WorktreeManifest
	manifestPath            string
	healthPath              string
	resolvedPath            string
	sourceUpdateSummaryPath string
	dockerLogPath           string
	dockerSummaryPath       string
	failureStage            string
	failureTarget           string
	errObj                  *runrecord.ErrorObject
	paths                   map[string]string
	recorder                artifact.Recorder
	exitCode                int
}

func archiveBuildFailure(ctx context.Context, input archiveFailureInput) (Result, int) {
	_ = input.runStore.StartStage(input.runDir, "build.result", "判定构建结果", time.Now().UTC())
	_ = input.runStore.FinishStage(input.runDir, "build.result", runrecord.StatusFailed, []string{input.dockerLogPath, input.dockerSummaryPath}, input.errObj, input.errObj.Suggestion, time.Now().UTC())
	_ = input.runStore.StartStage(input.runDir, "failure.diagnose", "生成失败诊断上下文", time.Now().UTC())
	failureResult, err := input.recorder.ArchiveFailure(ctx, artifact.FailureInput{
		ProjectRoot:             input.root,
		Resolved:                input.resolved,
		Manifest:                input.manifest,
		ManifestPath:            input.manifestPath,
		HealthReportPath:        input.healthPath,
		ResolvedConfigPath:      input.resolvedPath,
		SourceUpdateSummaryPath: input.sourceUpdateSummaryPath,
		DockerLogPath:           input.dockerLogPath,
		DockerSummaryPath:       input.dockerSummaryPath,
		RunDir:                  input.runDir,
		FailureStage:            input.failureStage,
		FailureTarget:           input.failureTarget,
		ErrorCode:               input.errObj.Code,
		ErrorMessage:            input.errObj.Message,
	})
	if err != nil {
		archiveErr := artifactErrorObject(err)
		_ = input.runStore.FinishStage(input.runDir, "failure.diagnose", runrecord.StatusFailed, nil, archiveErr, archiveErr.Suggestion, time.Now().UTC())
		_ = input.runStore.Complete(input.runDir, runrecord.FinalBlocked, "failure-diagnose-failed", archiveErr, time.Now().UTC())
		return resultFromRun(input.command, input.root, input.record, archiveErr, input.paths, "blocked"), ExitWorkspaceError
	}
	input.paths["failure_index"] = failureResult.FailureIndexPath
	input.paths["diagnostics_dir"] = failureResult.DiagnosticsDir
	_ = input.runStore.SetPath(input.runDir, "failure_index", failureResult.FailureIndexPath)
	_ = input.runStore.SetPath(input.runDir, "diagnostics_dir", failureResult.DiagnosticsDir)
	_ = input.runStore.FinishStage(input.runDir, "failure.diagnose", runrecord.StatusSucceeded, []string{failureResult.FailureIndexPath}, nil, "", time.Now().UTC())
	_ = input.runStore.StartStage(input.runDir, "artifact.archive", "归档失败现场", time.Now().UTC())
	_ = input.runStore.FinishStage(input.runDir, "artifact.archive", runrecord.StatusSucceeded, []string{failureResult.FailureIndexPath}, nil, "", time.Now().UTC())
	_ = input.runStore.Complete(input.runDir, runrecord.FinalFailed, "build-failed", input.errObj, time.Now().UTC())
	return resultFromRun(input.command, input.root, input.record, input.errObj, input.paths, "failed"), input.exitCode
}

func finishBlocked(command, root string, record runrecord.RunRecord, store runrecord.Store, runDir, stageID, reason string, errObj *runrecord.ErrorObject, paths map[string]string, code int) (Result, int) {
	return finishBlockedWithResults(command, root, record, store, runDir, stageID, reason, errObj, paths, nil, code)
}

func finishBlockedWithResults(command, root string, record runrecord.RunRecord, store runrecord.Store, runDir, stageID, reason string, errObj *runrecord.ErrorObject, paths map[string]string, resultPaths []string, code int) (Result, int) {
	_ = store.FinishStage(runDir, stageID, runrecord.StatusFailed, resultPaths, errObj, errObj.Suggestion, time.Now().UTC())
	_ = store.Complete(runDir, runrecord.FinalBlocked, reason, errObj, time.Now().UTC())
	return resultFromRun(command, root, record, errObj, paths, "blocked"), code
}

func finishFailed(command, root string, record runrecord.RunRecord, store runrecord.Store, runDir, stageID, reason string, errObj *runrecord.ErrorObject, paths map[string]string, code int) (Result, int) {
	_ = store.FinishStage(runDir, stageID, runrecord.StatusFailed, nil, errObj, errObj.Suggestion, time.Now().UTC())
	_ = store.Complete(runDir, runrecord.FinalFailed, reason, errObj, time.Now().UTC())
	return resultFromRun(command, root, record, errObj, paths, "failed"), code
}

func finishBlockedNoStageFailure(command, root string, record runrecord.RunRecord, store runrecord.Store, runDir, reason string, errObj *runrecord.ErrorObject, paths map[string]string, code int) (Result, int) {
	_ = store.Complete(runDir, runrecord.FinalBlocked, reason, errObj, time.Now().UTC())
	return resultFromRun(command, root, record, errObj, paths, "blocked"), code
}

func buildConfigErrorObject(err error) *runrecord.ErrorObject {
	if cfgErr, ok := buildconfig.AsError(err); ok {
		return &runrecord.ErrorObject{
			Code:       cfgErr.Code,
			Message:    cfgErr.Message,
			Suggestion: cfgErr.Suggestion,
			Details:    cfgErr.Details,
		}
	}
	return workspaceErrorObject("BUILD_CONFIG_ERROR", "构建配置生成失败", "检查配置 fragment、packages 和当前 run 工作树权限后重试", err)
}

func dockerErrorObject(err error) *runrecord.ErrorObject {
	if dockerErr, ok := dockerexec.AsError(err); ok {
		return &runrecord.ErrorObject{
			Code:       dockerErr.Code,
			Message:    dockerErr.Message,
			Suggestion: dockerErr.Suggestion,
			Details:    dockerErr.Details,
		}
	}
	return workspaceErrorObject("DOCKER_BUILD_ERROR", "Docker 构建执行失败", "查看 Docker 日志并检查构建环境后重试", err)
}

func dockerExitCode(err error) int {
	if dockerErr, ok := dockerexec.AsError(err); ok {
		if dockerErr.Kind == dockerexec.KindStartup {
			return ExitDockerError
		}
		if dockerErr.Kind == dockerexec.KindBuild {
			return ExitOpenWrtError
		}
	}
	return ExitWorkspaceError
}

func artifactErrorObject(err error) *runrecord.ErrorObject {
	if artifactErr, ok := artifact.AsError(err); ok {
		return &runrecord.ErrorObject{
			Code:       artifactErr.Code,
			Message:    artifactErr.Message,
			Suggestion: artifactErr.Suggestion,
			Details:    artifactErr.Details,
		}
	}
	return workspaceErrorObject("ARTIFACT_ARCHIVE_ERROR", "产物或诊断归档失败", "检查 workspace 权限和磁盘空间后重试", err)
}

func worktreeErrorObject(err error) *runrecord.ErrorObject {
	var wtErr *source.WorktreeError
	if errors.As(err, &wtErr) {
		return &runrecord.ErrorObject{
			Code:       wtErr.Code,
			Message:    wtErr.Message,
			Suggestion: wtErr.Suggestion,
			Details:    wtErr.Details,
		}
	}
	return &runrecord.ErrorObject{
		Code:       "WORKTREE_PREPARE_ERROR",
		Message:    "运行工作树或插件接入失败",
		Suggestion: "检查 source-set cache、workspace 权限和插件路径后重试",
		Details:    map[string]any{"error": err.Error()},
	}
}

func workspaceErrorObject(code, message, suggestion string, err error) *runrecord.ErrorObject {
	return &runrecord.ErrorObject{
		Code:       code,
		Message:    message,
		Suggestion: suggestion,
		Details:    map[string]any{"error": err.Error()},
	}
}

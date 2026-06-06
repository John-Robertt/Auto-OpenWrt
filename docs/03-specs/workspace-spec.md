---
status: accepted
owner: engineering
last_updated: 2026-06-07
depends_on:
  - docs/02-architecture/data-model.md
  - docs/03-specs/config-spec.md
  - docs/03-specs/run-record-state-spec.md
  - docs/04-decisions/ADR-0004-run-worktree-isolation.md
  - docs/04-decisions/ADR-0006-source-set-config-isolation.md
---

# 项目根目录与工作区规格

## 模型职责

项目根目录是 Auto-OpenWrt 的外层目录，负责保存用户配置、按 source-set 隔离的源码缓存、共享缓存和项目级运行记录。

工作区是同一项目根目录内由配置文件声明的长期状态边界，路径为 `workspaces/<workspace-id>/`。每个工作区负责保存 resolved config、运行工作树逻辑索引、构建记录、诊断、检查点、adopted patches、success lock 和产物。

## 目录结构

`init` 必须创建以下目录。目录已存在时保持幂等，不删除用户文件。

```text
<project-root>/
  configs/
    auto-openwrt.yaml
  sources/
    source-sets/
  cache/
    downloads/
    build/
  runs/
    doctor/
  workspaces/
    <workspace-id>/
      config/
        resolved/
      worktrees/
      runs/
        update/
      artifacts/
        .staging/
      diagnostics/
      checkpoints/
      patches/
        adopted/
      locks/
```

## 目录职责

- `configs/`：用户配置文件。
- `sources/source-sets/<source-set-id>/`：OpenWrt、feeds、plugins 的源码缓存命名空间。
- `cache/downloads/`：OpenWrt 下载缓存。
- `cache/build/`：Docker 构建缓存。
- `runs/doctor/<run-id>/`：未绑定配置文件的项目级 doctor 运行记录。
- `workspaces/<workspace-id>/config/resolved/<build-id>/<run-id>.yaml`：resolved config。
- `workspaces/<workspace-id>/worktrees/<build-id>/<run-id>/`：当前 run 工作树逻辑目录，保存源码物理目录或 storage pointer；权威 worktree manifest 位于对应 run record 目录。
- `workspaces/<workspace-id>/runs/<build-id>/<run-id>/`：build run record、日志、worktree manifest、Docker 摘要和健康检查报告。
- `workspaces/<workspace-id>/runs/doctor/<run-id>/`：绑定配置文件的 doctor 运行记录。
- `workspaces/<workspace-id>/runs/update/<run-id>/`：update 运行记录和 `source-update-summary.json`。
- `workspaces/<workspace-id>/artifacts/<build-id>/<run-id>/`：成功构建产物。
- `workspaces/<workspace-id>/artifacts/.staging/<build-id>/<run-id>/`：成功产物 finalize 前的 staging 目录。
- `workspaces/<workspace-id>/diagnostics/<build-id>/<run-id>/`：失败诊断上下文和失败索引。
- `workspaces/<workspace-id>/checkpoints/<build-id>/<run-id>/round-<n>/`：AI 修复前检查点。
- `workspaces/<workspace-id>/patches/adopted/<build-id>/`：AI 修复成功后自动采纳的 `workspace_id/build_id` 级补丁。
- `workspaces/<workspace-id>/locks/<build-id>/success-lock.json`：最近一次成功构建版本记录。

## 工作树逻辑与物理位置

`workspaces/<workspace-id>/worktrees/<build-id>/<run-id>/` 是逻辑标识，不一定是源码物理目录。

物理 storage driver：

- `host-path`：源码物理目录为 `<project-root>/workspaces/<workspace-id>/worktrees/<build-id>/<run-id>/source`。
- `docker-volume`：源码物理目录为 Docker volume 中的 `/openwrt`，宿主逻辑目录只保存 storage pointer。
- `linux-path`：源码物理目录为 `workspace.linux_worktree_root/<workspace-id>/<build-id>/<run-id>/source`。

Worktree Manifest 必须记录：

- logical worktree id：`workspaces/<workspace-id>/worktrees/<build-id>/<run-id>/`。
- `workspace_id`。
- source set id。
- storage driver。
- physical source path 或 Docker volume name。
- container worktree path，固定为 `/openwrt`。
- case-sensitive 检查结果。
- source-set cache source commit。
- applied adopted patch ids。
- created time。

Worktree Manifest 的权威路径为 `workspaces/<workspace-id>/runs/<build-id>/<run-id>/worktree-manifest.json`。工作树逻辑目录不得保存另一份 manifest 副本，只能保存指向权威 manifest 或物理源码位置的 pointer。

## 隔离规则

- 每个 `workspace_id/build_id` 必须拥有独立 run record。
- 每个 run 必须拥有独立运行工作树逻辑标识和物理源码位置。
- 不同 `workspace_id/build_id` 的产物、日志、运行工作树、success lock 和 adopted patches 不得覆盖或混淆。
- 失败构建不得覆盖上一次 success lock。
- AI 修复必须关联到具体 run record 和当前 run 工作树。
- AI 修复成功后的差异只能通过 adopted patch 进入长期状态。
- `sources/source-sets/<source-set-id>/` 是源码缓存，不允许被 Docker 构建、构建配置生成或 AI 修复写入。

## 写入规则

- 写 JSON/YAML 记录时必须先写临时文件，再原子 rename。
- 创建 run record 后，后续阶段只能更新同一 run record，不得创建第二个代表同一构建的 run。
- 成功产物和失败诊断必须写入不同目录。
- 清理策略不得删除仍被 success lock 引用的 `sources/source-sets/` 源码基线、`workspaces/<workspace-id>/locks/` 或 `workspaces/<workspace-id>/patches/adopted/`。
- `workspaces/<workspace-id>/worktrees/<build-id>/<run-id>/` 及其物理源码位置可以在用户确认后清理，但清理前 run record 必须保留最终状态、worktree manifest、最后现场摘要和清理时间。

## 崩溃恢复边界

- 存在 artifact staging 目录但 run final status 不是 `succeeded` 时，该 staging 目录不得被 `logs --latest` 视为成功产物。
- 存在 worktree manifest 但 run final status 缺失时，下一次 mutating command 必须按 Run Record 规格把该 run 标记为 `blocked`。
- 存在 success lock 但对应 run final status 不是 `succeeded` 时，项目状态一致性检查必须报告错误。

## 验收

- 一个项目根目录可以维护多个配置文件；每个配置文件拥有独立工作区。
- 一个工作区可以维护多个 build。
- 每次构建都能追溯到配置、源码版本、逻辑工作树、物理源码位置、日志和产物。
- `docker-volume` 模式下，宿主逻辑工作树目录存在且 manifest 可读。
- 失败现场和成功产物都能被独立定位。
- 同一项目根目录内两个配置文件存在同名 build 时，run record、worktree、日志、产物、success lock 和 adopted patches 不互相覆盖。

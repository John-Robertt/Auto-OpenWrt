---
status: accepted
owner: engineering
last_updated: 2026-06-06
depends_on:
  - docs/02-architecture/build-pipeline.md
  - docs/03-specs/artifact-spec.md
  - docs/03-specs/cli-spec.md
  - docs/03-specs/config-spec.md
  - docs/03-specs/docker-executor-spec.md
  - docs/03-specs/health-check-spec.md
  - docs/03-specs/run-record-state-spec.md
  - docs/03-specs/source-plugin-spec.md
  - docs/03-specs/workspace-spec.md
  - docs/04-decisions/ADR-0004-run-worktree-isolation.md
---

# Phase 2：构建流水线

## 目标

实现从 run record 创建、预检、共享源码更新、运行工作树准备、feeds/plugins 接入、构建上下文校验、构建配置生成、Docker 构建到产物归档的主流程。

本计划依赖的工程规格已为 `accepted`，可以作为 Phase 2 代码实现准入。

## 交付

- `build` 命令。
- `update` 命令。
- Run Record。
- Source Manager：OpenWrt、feeds、plugins 共享源码缓存更新和版本快照。
- Plugin Manager：feeds/plugins 接入和风险识别。
- 运行工作树准备和 worktree manifest。
- adopted patches 应用入口。
- 构建上下文校验。
- Docker 构建执行器和 Docker 环境摘要。
- Success Lock。
- 成功和失败产物归档。

## 边界

- 不实现 AI CLI 调用。
- 构建失败后只生成诊断上下文，不进入自动修复。
- adopted patch 应用入口按 success lock 支持，但 patch 生成由 Phase 3 完成。
- 不实现清理策略。

## 验收

- `update` 可以更新配置中启用的 OpenWrt、feeds 和插件共享源码缓存。
- `build --profile <name>` 可以按指定 build profile 发起构建。
- 每次构建拥有独立 run record 和运行工作树。
- 构建前自动执行预检。
- 运行工作树准备后执行构建上下文校验。
- Worktree Manifest 记录 logical id、storage driver、物理路径或 Docker volume。
- Docker Executor 只挂载当前 run 工作树、缓存和产物目录。
- 成功后记录固件、日志、resolved config、health report、Docker 环境摘要、worktree manifest 和 success lock。
- 失败后保留失败日志、worktree manifest、诊断上下文和 failure-index。
- 失败构建不覆盖上一次 success lock。
- success lock 写入失败时，run final status 不得为 `succeeded`。

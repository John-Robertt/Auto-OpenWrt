---
status: accepted
owner: engineering
last_updated: 2026-06-06
depends_on:
  - docs/03-specs/config-spec.md
  - docs/03-specs/cli-spec.md
  - docs/03-specs/workspace-spec.md
  - docs/03-specs/health-check-spec.md
  - docs/03-specs/run-record-state-spec.md
---

# Phase 1：基础能力

## 目标

建立 Go CLI、配置解析、工作区初始化、基础健康检查和文档约束，让后续构建流水线有稳定输入。

本计划依赖的 `03-specs/` 已为 `accepted`，可以作为 Phase 1 代码实现准入。

## 交付

- CLI 基础命令：`init`、`doctor`。
- Workspace Store：创建并校验工作区目录结构。
- YAML user config 解析和 resolved config 输出。
- Run id 生成、doctor health report 写入。
- Run Record 基础状态写入和中断恢复标记。
- 基础 Health Report，覆盖 Docker、权限、工作区目录、磁盘、网络、配置和 AI CLI 可用性。
- 文档静态检查：front matter、依赖存在性、依赖图无环、README 状态一致性。

## 边界

- 不执行 OpenWrt 构建。
- 不更新共享源码缓存。
- 不创建 build run 工作树。
- 不调用 AI 修复。
- 不写 success lock。

## 验收

- `auto-openwrt init` 可以创建工作区。
- 重复执行 `init` 不破坏已有目录和记录。
- 可以解析示例配置，并显式输出 `ai_repair.adoption: auto`。
- resolved config 显式输出 `workspace.worktree_storage`、`docker.image` 和 `docker.platform`。
- 配置错误包含字段路径、原因和修复建议。
- `auto-openwrt doctor` 可以执行预检并输出报告。
- 失败项包含原因和建议。
- 初始化后的工作区目录能支撑多 profile 和 run 级工作树隔离。
- 中断的 doctor/update run 在下一次 mutating command 前可被标记为 `blocked`。
- 文档静态检查能发现缺失 front matter、坏依赖、依赖环和 README 状态不一致。

---
status: accepted
owner: architecture
last_updated: 2026-06-06
depends_on:
  - docs/02-architecture/architecture-design.md
  - docs/04-decisions/ADR-0004-run-worktree-isolation.md
  - docs/04-decisions/ADR-0005-ai-repair-auto-adoption.md
---

# 核心数据模型

## 契约总则

Auto-OpenWrt 的长期状态全部归属到 Workspace。用户配置采用 YAML；运行时记录、索引和 lock 采用 JSON；补丁采用 unified diff patch。

ID 和路径规则：

- `profile` 名称必须匹配 `[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}`。
- `run_id` 格式为 `YYYYMMDDTHHMMSSZ-<6位小写字母或数字>`，由系统创建。
- `patch_id` 格式为 `patch-YYYYMMDDTHHMMSSZ-<6位小写字母或数字>`，由系统创建。
- 所有运行时 JSON 必须包含 `schema_version`，v1 固定为 `1`。
- 所有持久化写入必须先写入同目录临时文件，再原子 rename 到目标路径。
- 失败构建不得覆盖已有 success lock、成功 artifact index 或 adopted patch。

状态枚举：

- 阶段状态：`pending`、`running`、`succeeded`、`failed`、`skipped`。
- run 最终状态：`succeeded`、`failed`、`blocked`。
- 健康检查项状态：`pass`、`warn`、`fail`。
- adopted patch 采纳结果：`not_applicable`、`adopted`、`failed`。
- 插件风险类型：`luci-app`、`kernel-module`、`patch`、`unknown`。

## Workspace

Workspace 表示一个长期维护的 OpenWrt 构建项目。

核心字段：

- 项目名称。
- 工作区路径。
- user config 路径。
- 共享源码缓存路径。
- 运行工作树根目录。
- Docker volume 名称或 Linux 原生构建路径。
- OpenWrt 源码状态。
- feeds 源码状态。
- 插件源码状态。
- build profiles。
- 构建缓存位置。
- 构建记录列表。
- 产物目录。
- 诊断目录。
- AI 修复检查点目录。
- adopted patch 目录和索引。

## Build Profile

Build Profile 表示一套固件构建目标。

核心字段：

- profile 名称。
- OpenWrt target。
- subtarget。
- device profile。
- 启用 feeds。
- 启用插件。
- OpenWrt 配置片段。
- 构建选项。
- AI 修复策略。
- 产物归档策略。
- adopted patch 应用策略。

## Plugin Source

Plugin Source 表示一个自定义插件或 feeds 包。

核心字段：

- 名称。
- 类型：`feed`、`package`、`patch`、`unknown`。
- 仓库地址。
- 分支。
- 路径。
- 启用状态。
- 风险类型：`luci-app`、`kernel-module`、`patch`、`unknown`。
- 当前源码版本。
- 最近成功构建版本。

## User Config

User Config 是用户维护的声明式配置。

持久化契约：

- 格式：YAML。
- 默认路径：`config/auto-openwrt.yaml`。
- 创建者：`init` 命令或用户手工维护。
- 写入规则：`init` 不得覆盖已有文件，除非用户显式传入覆盖参数。
- 读取者：Config/Profile Resolver。

最小内容：

- `version`。
- `workspace`。
- `openwrt`。
- `docker`。
- `profiles`。
- `feeds`。
- `plugins`。
- `health`。
- `ai_repair`。
- `artifacts`。

## Resolved Config

Resolved Config 是本次 run 的完整配置快照，包含用户配置、默认值、profile 展开结果、风险信息和将要应用的 adopted patches。

持久化契约：

- 格式：YAML。
- 路径：`config/resolved/<profile>/<run-id>.yaml`。
- 创建者：Config/Profile Resolver。
- 写入时机：run record 创建后、预检前。
- 写入规则：同一 `run_id` 只能写入一次；后续阶段只引用路径，不修改内容。

必须包含：

- `schema_version`。
- `run_id`。
- `profile`。
- 完整 `workspace`、`openwrt`、`feeds`、`plugins`、`health`、`ai_repair`、`artifacts`。
- 展开后的 `build` 选项。
- 插件风险类型。
- 将要应用的 adopted patch ids。

## Worktree Manifest

Worktree Manifest 表示当前 run 工作树的来源、位置和可回溯信息。

持久化契约：

- 格式：JSON。
- 路径：`runs/<profile>/<run-id>/worktree-manifest.json`。
- 创建者：Source Manager。
- 写入时机：运行工作树准备完成后。
- 更新规则：应用 adopted patches 后允许更新一次，进入 Docker 构建后不得再改。

必须包含：

- `schema_version`。
- `profile`。
- `run_id`。
- logical worktree id。
- storage driver：`host-path`、`docker-volume`、`linux-path`。
- 物理源码路径或 Docker volume 名称。
- 容器工作树路径。
- 大小写敏感能力和文件系统风险摘要。
- 来源源码缓存版本。
- 已应用的 adopted patch ids。
- 准备时间。

## Health Report

Health Report 表示一次健康检查结果。

持久化契约：

- 格式：JSON。
- `doctor` 独立执行路径：`runs/doctor/<run-id>/health-report.json`。
- `build` 执行路径：`runs/<profile>/<run-id>/health-report.json`。
- 创建者：Health Checker。
- 写入规则：预检和构建上下文校验写入同一报告的不同 section。

必须包含：

- `schema_version`。
- 检查时间。
- 关联 workspace。
- 关联 build profile，可为空。
- 关联 run id。
- `preflight` 检查项列表。
- `build_context` 检查项列表。
- 每项状态：`pass`、`warn`、`fail`。
- 失败原因。
- 修复建议。
- 是否允许继续构建。

## Run Record

Run Record 表示一次构建尝试。

持久化契约：

- 格式：JSON。
- 路径：`runs/<profile>/<run-id>/run.json`。
- 创建者：Build Application / Pipeline。
- 写入规则：创建后可追加阶段状态；每次更新必须原子写入；最终状态只能写入一次。

必须包含：

- `schema_version`。
- `run_id`。
- `profile`。
- 开始时间和结束时间。
- 固定 stage id 对应的流水线阶段状态。
- worktree manifest 路径。
- health report 路径。
- resolved config 路径。
- 源码版本快照。
- Docker image、platform、volume 和路径映射摘要。
- 构建日志路径。
- 产物路径。
- 失败诊断路径。
- checkpoint 列表。
- AI repair diff 列表。
- adoption result。
- AI 修复历史。
- 最终状态。

## Checkpoint

Checkpoint 表示 AI 修复前当前 run 工作树的回退边界。

持久化契约：

- 格式：JSON metadata + patch 或 source snapshot reference。
- 路径：`checkpoints/<profile>/<run-id>/round-<n>/checkpoint.json`。
- 创建者：AI Repair Coordinator。
- 写入时机：每轮 AI 修复前。
- 写入规则：每个轮次只能创建一次，不能覆盖。

必须包含：

- `schema_version`。
- `checkpoint_id`。
- `profile`。
- `run_id`。
- 修复轮次。
- 工作树路径。
- 修复前源码版本和 dirty 状态。
- 可回退材料路径。

## Adopted Patch

Adopted Patch 表示一次成功 AI 修复被自动采纳后的可审计补丁。

持久化契约：

- 格式：unified diff patch + JSON metadata。
- patch 路径：`patches/adopted/<profile>/<patch-id>.patch`。
- metadata 路径：`patches/adopted/<profile>/<patch-id>.json`。
- profile 索引路径：`patches/adopted/<profile>/index.json`。
- 创建者：Artifact Recorder。
- 写入时机：AI 修复后的构建成功后、Success Lock 写入前。
- 写入规则：patch 和 metadata 写入成功后，才能更新 profile 索引。

必须包含：

- `patch_id`。
- `profile`。
- 来源 run id。
- 来源 AI 修复轮次。
- patch 文件路径。
- diff 摘要。
- 采纳时间。
- 关联 success lock。

## Success Lock

Success Lock 表示最近一次成功构建的版本状态。

持久化契约：

- 格式：JSON。
- 路径：`locks/<profile>/success-lock.json`。
- 创建者：Artifact Recorder。
- 写入时机：成功产物和 adopted patch 归档完成后。
- 写入规则：只有最终状态为 `succeeded` 的 run 可以更新；失败和 blocked run 不得写入。

必须包含：

- `schema_version`。
- 成功构建时间。
- `profile`。
- `run_id`。
- OpenWrt commit。
- feeds commit。
- 插件 commit。
- adopted patch ids。
- resolved config 摘要。
- 固件产物路径。
- 构建环境摘要。

## Artifact Index

Artifact Index 表示成功或失败 run 的可浏览索引。

持久化契约：

- 成功索引格式：JSON。
- 成功索引路径：`artifacts/<profile>/<run-id>/artifact-index.json`。
- 失败索引格式：JSON。
- 失败索引路径：`diagnostics/<profile>/<run-id>/failure-index.json`。
- 创建者：Artifact Recorder 或 Failure Diagnostics。
- 写入规则：成功索引不得被失败 run 覆盖；失败索引不得写入 success lock。

成功索引必须包含：

- 固件路径。
- 构建日志路径。
- resolved config 路径。
- health report 路径。
- success lock 路径。
- 源码版本记录路径。
- Docker 环境摘要。
- worktree manifest 路径。
- adopted patch ids 或 adopted patch 索引路径。

失败索引必须包含：

- 失败日志路径。
- 失败阶段。
- 失败包或失败目标。
- 诊断上下文路径。
- health report 路径。
- resolved config 路径。
- worktree manifest 路径。
- checkpoint 索引路径。
- AI 修复历史路径。
- 最后现场摘要。

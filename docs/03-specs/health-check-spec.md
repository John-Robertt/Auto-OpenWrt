---
status: accepted
owner: engineering
last_updated: 2026-06-07
depends_on:
  - docs/01-product/product-planning.md
  - docs/02-architecture/build-pipeline.md
  - docs/03-specs/config-spec.md
  - docs/03-specs/docker-executor-spec.md
  - docs/03-specs/run-record-state-spec.md
  - docs/03-specs/workspace-spec.md
  - docs/04-decisions/ADR-0006-source-set-config-isolation.md
---

# 健康检查规格

## 检查时机

健康检查分为两类：

- 预检：`doctor` 独立执行，或 `build` 在源码更新前自动执行。
- 构建上下文校验：`build` 在当前 run 工作树准备完成后执行。

`doctor` 不要求创建 build 的运行工作树；如果用户指定 `--build`，可以读取配置并检查 build 声明完整性，但不更新 source-set 源码缓存。

参数错误、配置文件无法读取、YAML 语法错误、配置 schema 错误、build 不存在、build 引用不存在或 disabled feed/plugin 返回 CLI 退出码 `2`。只有 Health Report 已成功生成且存在阻断项时，才返回退出码 `3`。

## 预检检查项

- `system.os`：宿主系统和 CPU 架构。
- `system.commands`：基础命令可用性，至少包含 `git`、`sh`、`docker`。
- `network.git`：访问源码仓库所需网络能力。
- `docker.installed`：Docker 是否安装。
- `docker.running`：Docker daemon 是否运行。
- `docker.permission`：当前用户是否可启动容器。
- `docker.image`：`docker.image` 是否非空，且可在需要时 pull 或 inspect。
- `docker.platform`：`docker.platform` 是否为 `auto` 或 Docker 支持的 platform。
- `project.read_write`：project root 是否可读写。
- `project.directories`：project root 必需目录是否存在或可创建。
- `workspace.read_write`：resolved workspace id 对应的长期状态目录是否可读写。
- `workspace.storage_driver`：resolved worktree storage driver 是否可用。
- `cache.read_write`：缓存目录是否可读写。
- `disk.available`：磁盘空间是否满足 `health.min_disk_gb`。
- `ai.command`：启用 AI 修复时，AI CLI 存在且可执行。

## 构建上下文校验项

- `worktree.manifest`：当前 run worktree manifest 存在且 schema_version 正确。
- `worktree.exists`：当前 run 工作树物理位置存在。
- `worktree.read_write`：当前 run 工作树可读写。
- `worktree.filesystem`：工作树存储满足构建需求，或给出风险提示。
- `openwrt.target`：target、subtarget、device profile 在 OpenWrt 源码上下文中有效。
- `docker.mapping`：Docker image、platform、volume 或宿主映射路径明确。
- `docker.mount_scope`：Docker 只挂载当前 run 工作树、缓存、artifact staging 和必要只读 source-set cache。
- `plugins.risk`：插件风险类型已解析并写入上下文。
- `plugins.attach`：feeds/plugins 接入材料存在。
- `ai.worktree_access`：启用 AI 修复时，AI CLI 可访问当前 run 工作树的宿主路径。

## 状态

每个检查项返回：

- `pass`：满足要求。
- `warn`：可继续，但存在风险。
- `fail`：阻断构建。

每项结果必须包含：

- `id`。
- `status`。
- `summary`。
- `detail`。
- `suggestion`。

## 阻断规则

- Docker 不可用时阻断构建。
- `docker.image` 为空或不可用时阻断构建。
- project root 或当前 workspace 状态目录无写权限时阻断构建。
- 当前 run 工作树存储不可用或不可写时阻断构建。
- target、subtarget、device profile 无法在 OpenWrt 源码上下文中验证通过时阻断构建。
- Docker mount 范围违反 Docker Executor 规格时阻断构建。
- plugin 接入材料缺失或 patch plugin 检查失败时阻断构建。
- AI 自动修复启用但 AI CLI 不可用、不可执行或无法访问当前 run 工作树时阻断构建。
- AI 自动修复启用且 resolved storage driver 为 `docker-volume` 时，v1 必须阻断并建议切换到 `host-path` 或 `linux-path`，因为外部 AI CLI 没有宿主可访问的源码路径。

macOS、WSL 或非 case-sensitive 文件系统上，如果 resolved storage driver 是 `host-path`，健康检查至少输出 `warn`，并给出切换到 `docker-volume` 或 `linux-path` 的建议。

## Health Report 输出

健康检查必须输出 Health Report。

- `doctor` 独立执行路径：`runs/doctor/<run-id>/health-report.json`。
- 绑定配置文件或 build 的 `doctor` 执行路径：`workspaces/<workspace-id>/runs/doctor/<run-id>/health-report.json`。
- `build` 执行路径：`workspaces/<workspace-id>/runs/<build-id>/<run-id>/health-report.json`。

Health Report 必须包含：

- `schema_version`。
- `run_id`。
- `workspace_id`，doctor 未指定配置时为 `null`。
- `source_set_id`，doctor 未指定 build 时为 `null`。
- `build_id`，doctor 未指定 build 时为 `null`。
- `project_root`。
- `preflight` 检查项列表。
- `build_context` 检查项列表。
- Docker image/platform 预期。
- worktree storage driver、logical id 和 physical path 或 volume。
- 插件风险类型。
- AI repair 可用性。
- `can_continue`。

## 验收

- Docker daemon 未运行时，`doctor` 输出 `docker.running: fail` 和修复建议。
- 配置 YAML 语法错误时，CLI 返回 `2` 且不伪造完整 Health Report。
- build 引用 disabled plugin 时，CLI 返回 `2`，不创建 run record，也不生成 Health Report。
- AI repair 启用但命令不可执行时，构建上下文校验阻断。
- 非 case-sensitive host path 作为构建工作树时，报告输出 `warn` 和迁移建议。
- Docker mount 范围包含 project root 时，构建上下文校验输出 `docker.mount_scope: fail`。

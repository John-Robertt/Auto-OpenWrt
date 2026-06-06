---
status: accepted
owner: engineering
last_updated: 2026-06-06
depends_on:
  - docs/02-architecture/build-pipeline.md
  - docs/03-specs/config-spec.md
  - docs/03-specs/source-plugin-spec.md
  - docs/03-specs/workspace-spec.md
  - docs/04-decisions/ADR-0002-docker-build-boundary.md
  - docs/04-decisions/ADR-0004-run-worktree-isolation.md
  - docs/04-decisions/ADR-0006-source-set-config-isolation.md
---

# Docker 执行器规格

## 职责边界

Docker Executor 只负责在当前 run 工作树中执行 OpenWrt 构建命令，并记录 Docker image、platform、volume、路径映射、退出码和日志。

Docker image 只表示基础构建环境，不包含 OpenWrt、feeds 或 plugins 源码。Docker Executor 不读取 user config 做产品决策，不更新 source-set 源码缓存，不写 success lock，不直接采纳 AI diff。

## 输入

执行请求必须来自 Build Application / Pipeline，并包含：

- `run_id`。
- `workspace_id`。
- `source_set_id`。
- `build_id`。
- resolved config 路径。
- Worktree Manifest。
- Docker image。
- Docker platform。
- feed/plugin attach summary。
- 构建 jobs。
- 构建配置片段路径，必须位于当前 run 工作树 `.auto-openwrt/config-fragments/`。
- 下载缓存路径。
- 构建缓存路径。
- artifact staging 路径。
- Docker 日志路径。

## Docker 配置

配置字段：

- `docker.image`：必填，非空字符串。
- `docker.platform`：默认 `auto`；允许 `auto` 或 Docker 支持的 platform 字符串，如 `linux/amd64`、`linux/arm64`。

解析规则：

- `docker.image` 缺失或为空时，配置解析失败，CLI 返回退出码 `2`。
- `docker.platform: auto` 时不向 `docker run` 传递 `--platform`。
- 非 `auto` platform 原样传递给 `docker run --platform`，并写入 run record。

## 挂载规则

Docker Executor 只允许挂载以下目录或 volume：

- 当前 run 工作树，容器路径固定为 `/openwrt`，读写。
- 下载缓存，容器路径固定为 `/openwrt/dl`，读写。
- 构建缓存，容器路径固定为 `/auto-openwrt/cache/build`，读写。
- artifact staging 目录，容器路径固定为 `/auto-openwrt/artifacts`，读写。
- feeds/plugins source-set 源码缓存，仅当 `src-link` 需要时挂载，容器路径固定为 `/auto-openwrt/sources/<source-set-id>/<kind>/<name>`，只读。

禁止挂载：

- 整个 project root。
- 其他 `workspace_id/build_id` 的工作树。
- 历史 run 工作树。
- 其他 source-set 的源码缓存。
- `locks/`。
- `patches/adopted/`。

所有挂载必须写入 run record 和 Docker 环境摘要。

## 构建命令

容器工作目录必须为 `/openwrt`。

v1 固定执行以下命令序列：

```sh
./scripts/feeds update -a
./scripts/feeds install -a
if ls /openwrt/.auto-openwrt/config-fragments/*.config >/dev/null 2>&1; then cat /openwrt/.auto-openwrt/config-fragments/*.config >> .config; fi
make defconfig
make -j<jobs> V=s
```

命令规则：

- `<jobs>` 来自 resolved config 的 `builds[].config.jobs`；`auto` 由宿主解析为可用 CPU 数。
- config fragments 目录为空时，config fragment 步骤必须跳过，不得失败。
- Docker Executor 必须使用 `sh -lc` 执行命令序列，并把每条命令写入 Docker 日志。
- stdout 和 stderr 必须同时写入 `workspaces/<workspace-id>/runs/<build-id>/<run-id>/logs/docker-build.log`。
- 构建产物收集由 Artifact Recorder 完成；Docker Executor 只保证容器内命令结束后 artifact staging 目录可读。

## 退出语义

- Docker CLI 不存在、daemon 不可用或权限不足：健康检查阶段阻断，CLI 返回 `3`。
- `docker run` 无法启动容器：Docker 执行环境失败，CLI 返回 `5`。
- 容器启动成功但 OpenWrt 构建命令非零退出：OpenWrt 构建失败，CLI 返回 `6`。
- Docker 日志无法写入：工作区状态目录读写失败，CLI 返回 `8`。

## Docker 环境摘要

必须写入：

- image。
- platform。
- container id 或失败时的 Docker invocation id。
- worktree storage driver。
- volume names。
- host paths。
- container paths。
- cache paths。
- artifact staging path。
- Docker CLI version。
- Docker daemon version。
- start time。
- end time。
- exit code。

## 验收

- Docker Executor 不挂载 project root。
- `docker.platform: auto` 不传递 `--platform`。
- 非 `auto` platform 写入 run record。
- 构建日志可从 run record 和 artifact index 追溯。
- Docker 启动失败返回退出码 `5`。
- OpenWrt 构建命令失败返回退出码 `6` 并触发失败诊断。

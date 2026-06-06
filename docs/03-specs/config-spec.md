---
status: accepted
owner: engineering
last_updated: 2026-06-06
depends_on:
  - docs/02-architecture/data-model.md
  - docs/04-decisions/ADR-0005-ai-repair-auto-adoption.md
---

# 配置规格

## 配置文件

用户配置采用 YAML，用于声明 workspace、OpenWrt 源码、Docker 执行环境、build profiles、feeds、plugins、构建选项、健康检查、AI 修复策略和产物归档策略。

文件契约：

- 默认路径：`config/auto-openwrt.yaml`。
- `init` 创建示例配置；已有文件默认不覆盖。
- `build` 和 `update` 可通过 `--config <path>` 指定配置。
- 所有相对路径都以 workspace root 为基准解析。
- YAML 语法错误、顶层结构不是 map、未知 `version` 时，配置解析失败并返回 CLI 退出码 `2`。

## 顶层结构

```yaml
version: 1

workspace:
  name: auto-openwrt
  worktree_storage: auto
  linux_worktree_root: ""

openwrt:
  repo: https://github.com/openwrt/openwrt.git
  branch: openwrt-24.10
  update: latest

docker:
  image: ghcr.io/auto-openwrt/build-env:openwrt-24.10
  platform: auto

profiles:
  - name: x86-64
    target: x86
    subtarget: "64"
    profile: generic
    feeds:
      - packages
      - luci
    plugins:
      - openclash
    build:
      config_fragments: []
      packages: []
      jobs: auto

feeds:
  - name: packages
    repo: https://github.com/openwrt/packages.git
    branch: openwrt-24.10
    path: feeds/packages
    enabled: true

plugins:
  - name: openclash
    type: package
    repo: https://github.com/vernesong/OpenClash.git
    branch: master
    path: luci-app-openclash
    enabled: true
    risk: luci-app

health:
  min_disk_gb: 80

ai_repair:
  enabled: false
  command: ""
  args: []
  timeout: 30m
  max_retries: 5
  adoption: auto

artifacts:
  retention: keep-all
```

## 顶层字段

- `version` 必须为 `1`。
- `workspace`、`openwrt`、`docker`、`profiles`、`health`、`ai_repair`、`artifacts` 必须存在。
- `feeds` 和 `plugins` 可以为空列表；空列表表示不声明自定义 feed 或 plugin。
- 未声明的可选字段必须在 resolved config 中显式输出默认值。

## Workspace 字段

- `workspace.name` 必须非空，并用于生成 Docker volume 名称；允许值匹配 `[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}`。
- `workspace.worktree_storage` 允许值：`auto`、`host-path`、`docker-volume`、`linux-path`；默认 `auto`。
- `workspace.linux_worktree_root` 默认空字符串。
- 当 `workspace.worktree_storage: linux-path` 时，`workspace.linux_worktree_root` 必须为绝对路径。
- `auto` 选择规则：Linux 且 workspace 文件系统 case-sensitive 时使用 `host-path`；macOS、WSL 或非 case-sensitive 文件系统默认使用 `docker-volume`；用户显式配置 `linux_worktree_root` 时可选择 `linux-path`。
- 当 `ai_repair.enabled: true` 且 `auto` 解析为 `docker-volume` 时，配置解析仍可成功，但构建上下文校验必须在 `ai.worktree_access` 阻断并提示用户改用 `host-path` 或 `linux-path`。

## OpenWrt 字段

- `openwrt.repo` 必须非空。
- `openwrt.branch` 必须非空。
- `openwrt.update` v1 只支持 `latest`；默认 `latest`。

## Docker 字段

- `docker.image` 必须非空。
- `docker.platform` 默认 `auto`；允许 `auto` 或 Docker 支持的 platform 字符串。
- `docker.platform: auto` 表示不向 `docker run` 传递 `--platform`。

## Profile 字段

- `profiles` 至少包含一个 build profile。
- 每个 profile 必须声明 `name`、`target`、`subtarget`、`profile`。
- profile 名称必须匹配 `[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}`。
- profile 名称在 `profiles` 内必须唯一。
- `profiles[].feeds` 默认空列表；空列表表示不启用自定义 feeds。
- `profiles[].plugins` 默认空列表；空列表表示不启用自定义 plugins。
- profile 引用的 feed/plugin name 必须存在于顶层 `feeds` 或 `plugins`。
- profile 引用 disabled feed/plugin 时，配置解析失败并指出引用名称。

## Build 字段

- `profiles[].build.config_fragments` 默认空列表。
- `config_fragments` 每一项必须是 workspace-relative 文件路径；文件不存在时，构建配置生成阶段失败。
- `profiles[].build.packages` 默认空列表。
- `packages` 每一项必须是 OpenWrt package 名称；普通名称表示启用并生成 `CONFIG_PACKAGE_<name>=y`，以 `-` 开头表示禁用并生成 `# CONFIG_PACKAGE_<name> is not set`。
- `profiles[].build.jobs` 默认 `auto`；允许 `auto` 或大于等于 `1` 的整数。
- `jobs: auto` 由宿主解析为可用 CPU 数，并写入 resolved config。

## Feeds 字段

- `feeds[].name` 必须非空，匹配 `[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}`，并在 `feeds` 内唯一。
- `feeds[].repo` 必须非空。
- `feeds[].branch` 必须非空。
- `feeds[].path` 必须非空，用于记录 feed 在 OpenWrt 上下文中的路径标识。
- `feeds[].enabled` 默认 `true`。

## Plugins 字段

- `plugins[].name` 必须非空，匹配 `[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}`，并在 `plugins` 内唯一。
- `plugins[].type` 允许值：`feed`、`package`、`patch`、`unknown`；默认 `unknown`。
- `plugins[].repo` 必须非空。
- `plugins[].branch` 必须非空。
- `plugins[].path` 默认空字符串；空字符串表示仓库根目录。
- `plugins[].enabled` 默认 `true`。
- `plugins[].risk` 允许值：`luci-app`、`kernel-module`、`patch`、`unknown`；默认 `unknown`。

## Health 字段

- `health.min_disk_gb` 默认 `80`；必须为大于 `0` 的整数。

## AI CLI 配置

- `ai_repair.enabled` 默认 `false`。
- `ai_repair.command` 默认空字符串。
- `ai_repair.args` 默认空列表。
- `ai_repair.timeout` 默认 `30m`，必须能被解析为持续时间。
- `ai_repair.max_retries` 默认 `5`，允许范围 `0..5`。
- `ai_repair.adoption` v1 只允许 `auto`。

当 `ai_repair.enabled: true` 时：

- `ai_repair.command` 必须非空。
- `ai_repair.args` 可以为空；为空时系统把 AI 修复上下文文件路径作为唯一参数传入。
- `ai_repair.args` 支持占位符：`{context_file}`、`{worktree}`、`{run_id}`、`{profile}`、`{round}`。

## Artifacts 字段

- `artifacts.retention` v1 只支持 `keep-all`。

## Resolved Config

构建开始后必须生成 resolved config。resolved config 是本次构建使用的完整配置快照，并写入 run record。

输出契约：

- 格式：YAML。
- 路径：`config/resolved/<profile>/<run-id>.yaml`。
- 同一 `run_id` 只能写入一次。
- 必须显式记录所有默认值。
- 必须记录 `run_id`、`profile`、workspace root、logical worktree id 和 resolved worktree storage driver。
- 必须记录完整 `docker.image`、`docker.platform` 和 Docker mount 预期。
- 必须记录 `ai_repair.enabled`、`ai_repair.command`、`ai_repair.args`、`ai_repair.timeout`、`ai_repair.max_retries`、`ai_repair.adoption`。
- 必须记录 feed/plugin 风险信息和将要应用的 adopted patch ids。
- 生成后不得被后续阶段修改。

## 验收

- 示例配置可被解析并生成 resolved config。
- 未声明必填字段时，解析失败并返回用户可读错误。
- profile 名称重复时，解析失败并指出重复名称。
- profile 引用不存在或 disabled feed/plugin 时，解析失败并指出引用名称。
- `docker.image` 缺失或为空时，解析失败。
- `ai_repair.enabled: true` 且未声明 `command` 时，解析失败。
- resolved config 显式输出 `workspace.worktree_storage`、`docker.image`、`docker.platform`、`ai_repair.adoption: auto`、默认重试次数和 adopted patch ids。

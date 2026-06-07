---
status: accepted
owner: engineering
last_updated: 2026-06-07
depends_on:
  - docs/02-architecture/build-pipeline.md
  - docs/02-architecture/data-model.md
  - docs/03-specs/config-spec.md
  - docs/03-specs/workspace-spec.md
  - docs/04-decisions/ADR-0004-run-worktree-isolation.md
  - docs/04-decisions/ADR-0005-ai-repair-auto-adoption.md
  - docs/04-decisions/ADR-0006-source-set-config-isolation.md
---

# 源码与插件规格

## 职责边界

Source Manager 负责 OpenWrt、feeds 和 plugins 的 source-set 源码缓存更新、版本记录、运行工作树准备，以及按 success lock 应用 adopted patches。

Plugin Manager 负责把 resolved config 中声明的 feeds/plugins 接入当前 run 工作树，并解析插件风险类型。

二者不负责 Docker 构建、不判断是否重试、不写 success lock、不采纳 AI diff。

## Source-set 源码缓存

缓存路径：

- Source set metadata：`sources/source-sets/<source-set-id>/source-set.json`。
- OpenWrt：`sources/source-sets/<source-set-id>/openwrt/`。
- feeds：`sources/source-sets/<source-set-id>/feeds/<feed-name>/`。
- plugins：`sources/source-sets/<source-set-id>/plugins/<plugin-name>/`。

`source_set_id` 由 resolved config 中当前 build 的有效源码输入生成。不同 OpenWrt、feeds 或 plugins 来源组合必须使用不同 source-set cache；相同源码输入可以复用同一 source-set cache。

缓存规则：

- `openwrt.repo`、`feeds[].repo`、`plugins[].repo` 为空时，配置解析失败。
- 缓存目录不存在时，执行 `git clone --branch <branch> <repo> <cache-path>`。
- 缓存目录存在时，执行 `git fetch origin <branch>`，再 `git checkout <branch>` 和 `git reset --hard origin/<branch>`。
- 更新后执行 `git clean -fdx`，确保 source-set cache 不携带上次 run 的可变状态。
- `openwrt.update` v1 只支持 `latest`，表示每次 `update` 或 `build` 的源码更新阶段都取配置分支的远端最新 commit。
- source-set cache 更新失败时，不创建或继续准备当前 run 工作树，CLI 返回源码/工作树准备失败退出码 `4`。

版本快照必须记录：

- name。
- repo。
- branch。
- commit。
- cache path。
- source set id。
- update time。
- dirty state，成功更新后必须为 `false`。

## 运行工作树准备

运行工作树使用逻辑标识 `workspaces/<workspace-id>/worktrees/<build-id>/<run-id>/`。实际源码物理位置由 Worktree Manifest 的 storage driver 决定。

物理源码路径规则：

- `host-path`：`<project-root>/workspaces/<workspace-id>/worktrees/<build-id>/<run-id>/source`。
- `docker-volume`：Docker volume `auto-openwrt-<workspace-name>-<workspace-id>-<build-id>-<run-id>-worktree` 中的 `/openwrt`。
- `linux-path`：`workspace.linux_worktree_root/<workspace-id>/<build-id>/<run-id>/source`。

准备规则：

- 每个 run 只能创建一个运行工作树。
- 当前 run 工作树必须从 resolved config 指向的 source-set cache 的已记录 commit 创建。
- 当前 run 工作树必须是独立可写 Git working tree；AI 修复和构建配置生成只允许修改该工作树。
- `docker-volume` storage driver 必须通过一次性 helper container 把 source-set OpenWrt cache 拷贝到 volume 内，宿主不得把 Docker volume 当作普通目录直接写入。
- `linux-path` 只允许使用绝对路径，且该路径必须通过健康检查确认可写、case-sensitive，并归属于当前 workspace 的配置。
- 准备完成后写入 Worktree Manifest；进入 Docker 构建后不得再改变物理源码位置。

## Adopted Patch 应用

同 `workspace_id/build_id` 存在 success lock 时，Source Manager 必须按 success lock 中的 adopted patch ids 顺序应用补丁。

应用规则：

- 每个补丁先执行 `git apply --check`。
- 全部检查通过后按顺序执行 `git apply`。
- 任一补丁检查或应用失败时，本 run 阻断在运行工作树准备阶段，run 最终状态为 `blocked`，CLI 返回退出码 `4`。
- 应用结果写入 Worktree Manifest 和 run record。

## Feeds 接入

每个启用 feed 必须写入当前 run 工作树的 `feeds.conf`。

接入规则：

- 生成 `feeds.conf` 时，若工作树存在 `feeds.conf.default`，先复制其内容；resolved config 中同名 feed 覆盖默认条目。
- `host-path` 和 `linux-path` 直接在宿主可访问的当前 run 工作树中写入 `feeds.conf`。
- `docker-volume` 必须通过 helper container 读取 volume 内 `/openwrt/feeds.conf.default`，生成新的 `feeds.conf`，再写回 volume 内 `/openwrt/feeds.conf`；宿主不得把 Docker volume 当作普通目录直接写入。
- feed 类型使用 `src-link <name> <container-cache-path>`。
- Docker Executor 必须把 `sources/source-sets/<source-set-id>/feeds/<name>/` 以只读方式映射到 `<container-cache-path>`。
- 构建前必须在 Docker 内执行 `./scripts/feeds update -a` 和 `./scripts/feeds install -a`。
- disabled feed 不写入 `feeds.conf`，但可以保留 source-set cache。

## Plugin 接入

每个启用 plugin 按 `plugins[].type` 处理：

- `feed`：作为 feed 接入，写入 `feeds.conf`。
- `package`：把 `sources/source-sets/<source-set-id>/plugins/<name>/<path>` 拷贝到当前 run 工作树 `package/auto-openwrt/<name>/`。
- `patch`：把 `sources/source-sets/<source-set-id>/plugins/<name>/<path>` 下的 `.patch` 或 `.diff` 文件按文件名排序后应用到当前 run 工作树。
- `unknown`：按 `package` 处理，并把风险类型记录为 `unknown`。

`docker-volume` 规则：

- `feed` plugin 通过 helper container 更新 volume 内 `/openwrt/feeds.conf`。
- `package` 和 `unknown` plugin 通过 helper container 从只读 source-set cache 拷贝到 volume 内 `/openwrt/package/auto-openwrt/<name>/`。
- `patch` plugin 通过 helper container 在 volume 内 `/openwrt` 执行 `git apply --check` 和 `git apply`。
- helper container 使用 resolved `docker.image`；`docker.platform` 非 `auto` 时必须传递给 helper container。
- helper container 只能挂载当前 run Docker volume 和必要的只读 source-set cache，不得挂载 project root、locks 或 adopted patches 目录。

失败规则：

- plugin 源路径不存在时，`plugins.attach` 阶段失败。
- patch plugin 执行 `git apply --check` 失败时，`plugins.attach` 阶段失败。
- plugin 接入不得修改 `sources/source-sets/` 源码缓存。

## 风险识别

风险类型允许值：`luci-app`、`kernel-module`、`patch`、`unknown`。

识别优先级：

1. `plugins[].risk` 显式声明时直接采用。
2. type 为 `patch` 时标记为 `patch`。
3. package 路径或包名包含 `luci-app-` 时标记为 `luci-app`。
4. `Makefile` 包含 `KernelPackage/` 时标记为 `kernel-module`。
5. 其他情况标记为 `unknown`。

风险类型只用于提示、health report、run record、失败诊断和 AI 修复上下文，不阻止用户构建。

## 输出记录

Source Manager 必须输出：

- source version snapshot。
- source set id。
- source update summary。
- 运行工作树准备完成后的 worktree manifest。
- adopted patch application result。

Plugin Manager 必须输出：

- feed/plugin name。
- type。
- enabled state。
- source commit。
- source path。
- target path。
- risk type。
- attach status。

## 验收

- `update` 可以 clone 或 fetch OpenWrt、feeds 和 plugins，并记录 commit。
- `build` 可以从 source-set OpenWrt cache 创建独立 run 工作树。
- 同 `workspace_id/build_id` success lock 中的 adopted patches 按顺序应用。
- feed 使用 `src-link` 接入，Docker 中可执行 feeds update/install。
- package plugin 被拷贝到 `package/auto-openwrt/<name>/`。
- patch plugin 检查失败时阻断构建上下文校验。
- OpenClash 可被标记为 `luci-app`，Turbo ACC 类内核包可被标记为 `kernel-module` 或 `patch`。

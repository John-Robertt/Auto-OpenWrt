---
status: accepted
owner: engineering
last_updated: 2026-06-06
depends_on:
  - docs/02-architecture/build-pipeline.md
  - docs/02-architecture/data-model.md
  - docs/03-specs/config-spec.md
  - docs/03-specs/workspace-spec.md
  - docs/04-decisions/ADR-0004-run-worktree-isolation.md
  - docs/04-decisions/ADR-0005-ai-repair-auto-adoption.md
---

# 源码与插件规格

## 职责边界

Source Manager 负责 OpenWrt、feeds 和 plugins 的共享源码缓存更新、版本记录、运行工作树准备，以及按 success lock 应用 adopted patches。

Plugin Manager 负责把 resolved config 中声明的 feeds/plugins 接入当前 run 工作树，并解析插件风险类型。

二者不负责 Docker 构建、不判断是否重试、不写 success lock、不采纳 AI diff。

## 共享源码缓存

缓存路径：

- OpenWrt：`sources/openwrt/`。
- feeds：`sources/feeds/<feed-name>/`。
- plugins：`sources/plugins/<plugin-name>/`。

缓存规则：

- `openwrt.repo`、`feeds[].repo`、`plugins[].repo` 为空时，配置解析失败。
- 缓存目录不存在时，执行 `git clone --branch <branch> <repo> <cache-path>`。
- 缓存目录存在时，执行 `git fetch origin <branch>`，再 `git checkout <branch>` 和 `git reset --hard origin/<branch>`。
- 更新后执行 `git clean -fdx`，确保共享缓存不携带上次 run 的可变状态。
- `openwrt.update` v1 只支持 `latest`，表示每次 `update` 或 `build` 的源码更新阶段都取配置分支的远端最新 commit。
- 共享源码缓存失败时，不创建或继续准备当前 run 工作树，CLI 返回源码/工作树准备失败退出码 `4`。

版本快照必须记录：

- name。
- repo。
- branch。
- commit。
- cache path。
- update time。
- dirty state，成功更新后必须为 `false`。

## 运行工作树准备

运行工作树使用逻辑标识 `worktrees/<profile>/<run-id>/`。实际源码物理位置由 Worktree Manifest 的 storage driver 决定。

物理源码路径规则：

- `host-path`：`<workspace>/worktrees/<profile>/<run-id>/source`。
- `docker-volume`：Docker volume `auto-openwrt-<workspace-name>-<profile>-<run-id>-worktree` 中的 `/openwrt`。
- `linux-path`：`workspace.linux_worktree_root/<profile>/<run-id>/source`。

准备规则：

- 每个 run 只能创建一个运行工作树。
- 当前 run 工作树必须从 OpenWrt 共享缓存的已记录 commit 创建。
- 当前 run 工作树必须是独立可写 Git working tree；AI 修复和构建配置生成只允许修改该工作树。
- `docker-volume` storage driver 必须通过一次性 helper container 把共享 OpenWrt cache 拷贝到 volume 内，宿主不得把 Docker volume 当作普通目录直接写入。
- `linux-path` 只允许使用绝对路径，且该路径必须通过健康检查确认可写、case-sensitive，并归属于当前 workspace 的配置。
- 准备完成后写入 Worktree Manifest；进入 Docker 构建后不得再改变物理源码位置。

## Adopted Patch 应用

同 profile 存在 success lock 时，Source Manager 必须按 success lock 中的 adopted patch ids 顺序应用补丁。

应用规则：

- 每个补丁先执行 `git apply --check`。
- 全部检查通过后按顺序执行 `git apply`。
- 任一补丁检查或应用失败时，本 run 阻断在运行工作树准备阶段，run 最终状态为 `blocked`，CLI 返回退出码 `4`。
- 应用结果写入 Worktree Manifest 和 run record。

## Feeds 接入

每个启用 feed 必须写入当前 run 工作树的 `feeds.conf`。

接入规则：

- 生成 `feeds.conf` 时，若工作树存在 `feeds.conf.default`，先复制其内容；resolved config 中同名 feed 覆盖默认条目。
- feed 类型使用 `src-link <name> <container-cache-path>`。
- Docker Executor 必须把 `sources/feeds/<name>/` 以只读方式映射到 `<container-cache-path>`。
- 构建前必须在 Docker 内执行 `./scripts/feeds update -a` 和 `./scripts/feeds install -a`。
- disabled feed 不写入 `feeds.conf`，但可以保留共享缓存。

## Plugin 接入

每个启用 plugin 按 `plugins[].type` 处理：

- `feed`：作为 feed 接入，写入 `feeds.conf`。
- `package`：把 `sources/plugins/<name>/<path>` 拷贝到当前 run 工作树 `package/auto-openwrt/<name>/`。
- `patch`：把 `sources/plugins/<name>/<path>` 下的 `.patch` 或 `.diff` 文件按文件名排序后应用到当前 run 工作树。
- `unknown`：按 `package` 处理，并把风险类型记录为 `unknown`。

失败规则：

- plugin 源路径不存在时，构建上下文校验失败。
- patch plugin 执行 `git apply --check` 失败时，构建上下文校验失败。
- plugin 接入不得修改 `sources/` 共享缓存。

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
- worktree manifest。
- adopted patch application result。
- feed/plugin attach summary。

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
- `build` 可以从共享 OpenWrt cache 创建独立 run 工作树。
- 同 profile success lock 中的 adopted patches 按顺序应用。
- feed 使用 `src-link` 接入，Docker 中可执行 feeds update/install。
- package plugin 被拷贝到 `package/auto-openwrt/<name>/`。
- patch plugin 检查失败时阻断构建上下文校验。
- OpenClash 可被标记为 `luci-app`，Turbo ACC 类内核包可被标记为 `kernel-module` 或 `patch`。

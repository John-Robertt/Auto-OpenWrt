---
status: accepted
owner: architecture
last_updated: 2026-06-06
depends_on:
  - docs/01-product/product-planning.md
  - docs/04-decisions/ADR-0001-cli-workspace-pipeline.md
  - docs/04-decisions/ADR-0002-docker-build-boundary.md
  - docs/04-decisions/ADR-0004-run-worktree-isolation.md
---

# ADR-0006：按 workspace 与 source-set 隔离构建状态和源码缓存

## 背景

现有设计中，`sources/` 是 project root 级共享源码缓存，每次构建再从共享缓存创建当前 run 工作树。这可以隔离构建过程、AI 修复和失败现场，但不足以隔离多个配置文件声明的不同 OpenWrt、feeds 或 plugins 版本。

如果多个配置文件共用同一 project root，并且都使用同名 build，project root 级共享源码缓存、build 级 success lock 和 build 级 adopted patches 都可能让不同固件版本的长期状态互相影响。

Docker 可以保证基础构建环境一致，但不应把源码基线隐含进构建环境镜像；源码版本仍必须由配置、源码管理记录、worktree manifest 和 run record 明确追溯。

## 决策

引入两个隔离维度：

- `workspace_id`：配置文件的长期状态命名空间，用于隔离同一 project root 内不同配置文件的 run、产物、success lock、adopted patches、诊断和检查点。
- `source_set_id`：源码输入集合的缓存命名空间，由 resolved config 中有效源码输入生成，用于隔离不同 OpenWrt、feeds 和 plugins 来源组合。

Docker image 只表示构建环境，不包含 OpenWrt 源码、feeds 或 plugins 源码。源码仍由 Source Manager 更新、记录、复制到当前 run 工作树，并由 Docker Executor 挂载当前 run 工作树执行构建。

路径模型：

- 项目根目录：`<project-root>/`，保存用户配置、共享 source-set cache、共享 cache 和项目级 doctor 记录。
- 工作区状态：`<project-root>/workspaces/<workspace-id>/`，保存一个配置文件对应的长期状态。
- 源码缓存：`<project-root>/sources/source-sets/<source-set-id>/`，只作为可复用源码基线，不作为构建时可变源码区。
- 当前 run 工作树：`<project-root>/workspaces/<workspace-id>/worktrees/<build-id>/<run-id>/`，作为构建和 AI 修复的唯一可变源码边界。

## 理由

- 多配置文件可以在同一 project root 中构建不同固件版本，不会因共享源码缓存覆盖彼此基线。
- 相同源码输入的配置仍可复用同一 source-set cache，避免不必要的重复下载。
- `workspace_id/build_id` 隔离 success lock 和 adopted patches，避免同名 build 在不同配置文件之间串用成功状态。
- Docker 环境和源码基线职责分离，能同时获得环境一致性和源码可追溯性。
- 当前 run 工作树隔离仍然保留，继续作为构建、AI 修复和失败现场的唯一可变源码边界。

## 影响

- 配置解析必须确定 `workspace_id`，并把 `workspace_id` 和 `source_set_id` 写入 resolved config。
- Workspace 路径、run record、artifact、diagnostics、checkpoint、success lock 和 adopted patches 必须按 `workspaces/<workspace-id>/<build-id>` 分区。
- Source Manager 必须使用 `sources/source-sets/<source-set-id>/...` 管理 OpenWrt、feeds 和 plugins 缓存。
- Worktree Manifest 必须记录来源 `source_set_id`、源码快照和当前 run 工作树位置。
- Docker Executor 不从 Docker image 推断源码版本；Docker 环境摘要只记录构建环境，源码版本来自 source snapshot 和 run record。

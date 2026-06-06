---
status: accepted
owner: architecture
last_updated: 2026-06-06
depends_on:
  - docs/01-product/product-planning.md
  - docs/04-decisions/ADR-0001-cli-workspace-pipeline.md
  - docs/04-decisions/ADR-0002-docker-build-boundary.md
---

# ADR-0004：每次构建使用专属运行工作树

## 背景

产品需要在同一工作区维护多个 build profile，同时允许 AI 修复修改源码。如果所有 profile 和 run 共用同一份可变源码目录，构建状态、AI 修改和失败现场容易互相污染。

## 决策

`sources/` 只作为共享源码缓存。每次构建必须创建 `worktrees/<profile>/<run-id>/` 作为当前 run 的唯一逻辑工作树标识。

Docker 构建、构建配置生成和 AI 修复都只能作用于当前 run 工作树。当前 run 工作树的物理源码位置由 storage driver 决定：`host-path`、`docker-volume` 或 `linux-path`。macOS 或非大小写敏感文件系统上，构建工作树和缓存默认放在 Docker volume 或 Linux 原生文件系统中，并把实际位置写入 worktree manifest 和 run record。

## 理由

- 运行工作树让多 profile、多 run 和 AI 修改天然隔离。
- 共享源码缓存仍能减少重复下载和更新成本。
- Docker 路径映射和构建现场可以从 run record 精确回溯。
- macOS 文件系统风险可以被限制在可检查、可替换的存储边界内。

## 影响

- Run Record 必须记录 worktree manifest、逻辑工作树标识、storage driver、宿主路径或 Docker volume 名称。
- 健康检查必须覆盖运行工作树存储位置、权限和潜在文件系统风险。
- Docker 执行器只能挂载当前 run 工作树、缓存和产物目录。
- 清理策略必须以 profile 和 run id 为边界，不得删除共享源码缓存中的成功基线。

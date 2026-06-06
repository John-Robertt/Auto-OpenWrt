---
status: accepted
owner: engineering
last_updated: 2026-06-06
depends_on:
  - docs/02-architecture/data-model.md
  - docs/03-specs/config-spec.md
  - docs/03-specs/run-record-state-spec.md
  - docs/04-decisions/ADR-0004-run-worktree-isolation.md
---

# 工作区规格

## 工作区职责

工作区是 Auto-OpenWrt 的持久状态边界，负责保存配置、共享源码缓存、运行工作树逻辑索引、缓存、构建记录、诊断、检查点、adopted patches、success lock 和产物。

## 目录结构

`init` 必须创建以下目录。目录已存在时保持幂等，不删除用户文件。

```text
<workspace>/
  config/
    auto-openwrt.yaml
    resolved/
  sources/
    openwrt/
    feeds/
    plugins/
  worktrees/
  cache/
    downloads/
    build/
  runs/
    doctor/
    update/
  artifacts/
    .staging/
  diagnostics/
  checkpoints/
  patches/
    adopted/
  locks/
```

## 目录职责

- `config/`：用户配置和 resolved config。
- `sources/`：OpenWrt、feeds、plugins 共享源码缓存。
- `worktrees/<profile>/<run-id>/`：当前 run 工作树逻辑目录，至少保存 manifest 和 storage pointer。
- `cache/downloads/`：OpenWrt 下载缓存。
- `cache/build/`：Docker 构建缓存。
- `runs/<profile>/<run-id>/`：run record、日志、worktree manifest、Docker 摘要和健康检查报告。
- `runs/doctor/<run-id>/`：独立 doctor 运行记录。
- `runs/update/<run-id>/`：独立 update 运行记录。
- `artifacts/<profile>/<run-id>/`：成功构建产物。
- `artifacts/.staging/<profile>/<run-id>/`：成功产物 finalize 前的 staging 目录。
- `diagnostics/<profile>/<run-id>/`：失败诊断上下文和失败索引。
- `checkpoints/<profile>/<run-id>/round-<n>/`：AI 修复前检查点。
- `patches/adopted/<profile>/`：AI 修复成功后自动采纳的 profile 级补丁。
- `locks/<profile>/success-lock.json`：最近一次成功构建版本记录。

## 工作树逻辑与物理位置

`worktrees/<profile>/<run-id>/` 是逻辑标识，不一定是源码物理目录。

物理 storage driver：

- `host-path`：源码物理目录为 `<workspace>/worktrees/<profile>/<run-id>/source`。
- `docker-volume`：源码物理目录为 Docker volume 中的 `/openwrt`，宿主逻辑目录只保存 manifest。
- `linux-path`：源码物理目录为 `workspace.linux_worktree_root/<profile>/<run-id>/source`。

Worktree Manifest 必须记录：

- logical worktree id：`worktrees/<profile>/<run-id>/`。
- storage driver。
- physical source path 或 Docker volume name。
- container worktree path，固定为 `/openwrt`。
- case-sensitive 检查结果。
- cache source commit。
- applied adopted patch ids。
- created time。

## 隔离规则

- 每个 build profile 必须拥有独立 run record。
- 每个 run 必须拥有独立运行工作树逻辑标识和物理源码位置。
- 不同 profile 的产物、日志、运行工作树、success lock 和 adopted patches 不得覆盖或混淆。
- 失败构建不得覆盖上一次 success lock。
- AI 修复必须关联到具体 run record 和当前 run 工作树。
- AI 修复成功后的差异只能通过 adopted patch 进入长期状态。
- `sources/` 是共享源码缓存，不允许被 Docker 构建、构建配置生成或 AI 修复写入。

## 写入规则

- 写 JSON/YAML 记录时必须先写临时文件，再原子 rename。
- 创建 run record 后，后续阶段只能更新同一 run record，不得创建第二个代表同一构建的 run。
- 成功产物和失败诊断必须写入不同目录。
- 清理策略不得删除 `sources/` 中的成功基线、`locks/` 或 `patches/adopted/`。
- `worktrees/<profile>/<run-id>/` 及其物理源码位置可以在用户确认后清理，但清理前 run record 必须保留最终状态、worktree manifest、最后现场摘要和清理时间。

## 崩溃恢复边界

- 存在 artifact staging 目录但 run final status 不是 `succeeded` 时，该 staging 目录不得被 `logs --latest` 视为成功产物。
- 存在 worktree manifest 但 run final status 缺失时，下一次 mutating command 必须按 Run Record 规格把该 run 标记为 `blocked`。
- 存在 success lock 但对应 run final status 不是 `succeeded` 时，文档静态检查必须报告一致性错误。

## 验收

- 一个工作区可以维护多个 build profile。
- 每次构建都能追溯到配置、源码版本、逻辑工作树、物理源码位置、日志和产物。
- `docker-volume` 模式下，宿主逻辑工作树目录存在且 manifest 可读。
- 失败现场和成功产物都能被独立定位。
- 同一工作区两个 profile 构建时，run record、worktree、日志、产物、success lock 和 adopted patches 不互相覆盖。

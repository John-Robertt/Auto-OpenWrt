---
status: accepted
owner: engineering
last_updated: 2026-06-06
depends_on:
  - docs/02-architecture/data-model.md
  - docs/03-specs/run-record-state-spec.md
  - docs/03-specs/workspace-spec.md
  - docs/04-decisions/ADR-0005-ai-repair-auto-adoption.md
---

# 产物规格

## 成功产物

成功构建 finalize 后归档到：

```text
artifacts/<profile>/<run-id>/
```

必须包含：

- 完整固件镜像。
- 构建日志。
- resolved config。
- health report。
- success lock 路径或快照。
- 源码版本记录。
- Docker image、platform、volume 和路径映射摘要。
- worktree manifest。
- adopted patches 或 adopted patch 索引。
- `artifact-index.json`。

## 失败产物

失败构建后必须归档到：

```text
diagnostics/<profile>/<run-id>/
```

必须包含：

- 失败日志。
- 失败诊断上下文。
- health report。
- resolved config。
- worktree manifest。
- checkpoint 索引。
- AI 修复历史。
- AI repair diff 列表。
- 最后现场摘要。
- `failure-index.json`。

## Staging / Finalize

成功归档必须使用 staging 目录：

```text
artifacts/.staging/<profile>/<run-id>/
```

成功 finalize 顺序：

1. 固件、日志、resolved config、health report、源码版本记录、Docker 摘要、worktree manifest 写入 artifact staging 目录。
2. `artifact-index.json` 写入 artifact staging 目录，索引中的最终路径必须指向 `artifacts/<profile>/<run-id>/`。
3. 如果本 run 有成功 AI 修复，adopted patch 和 metadata 先写入 `patches/adopted/<profile>/.staging/<patch-id>/`。
4. 将 artifact staging 原子 rename 为 `artifacts/<profile>/<run-id>/`。
5. 将 adopted patch staging 原子 finalize 到 `patches/adopted/<profile>/<patch-id>.patch` 和 `.json`，再更新 profile patch index。
6. 原子写入 `locks/<profile>/success-lock.json`。
7. 更新 run record final status 为 `succeeded`。

可见性规则：

- `logs --latest` 只读取 final status 为 `succeeded`、`failed` 或 `blocked` 的 run。
- 存在 artifact final 目录但 run final status 不是 `succeeded` 时，不得视为成功产物。
- 存在 artifact staging 目录时，不得出现在 `logs --latest`。
- success lock 写入失败时，run final status 必须为 `blocked`，不得为 `succeeded`。

失败归档顺序：

1. 写入失败日志、诊断上下文、AI 修复历史和最后现场摘要。
2. 写入 `failure-index.json`。
3. 更新 run record final status 为 `failed` 或 `blocked`。
4. 不写 success lock，不写 adopted patch。

## 归档规则

- 产物必须关联 build profile 和 run id。
- 成功产物不得被失败构建覆盖。
- Success Lock 只在成功构建后更新。
- AI 修复成功后必须归档 adopted patch 并把 adopted patch id 写入 Success Lock。
- AI 修复失败时不得归档 adopted patch，不得更新 Success Lock。
- 用户可以通过 `logs` 命令找到最近一次 final 成功或失败记录。
- 任意成功产物都必须能追溯到 resolved config、源码版本、adopted patches、health report、构建日志和 Docker 环境摘要。

## artifact-index.json

成功索引必须包含：

- `schema_version`。
- `profile`。
- `run_id`。
- 固件路径列表。
- 构建日志路径。
- resolved config 路径。
- health report 路径。
- success lock 路径。
- 源码版本记录路径。
- Docker 环境摘要路径。
- worktree manifest 路径。
- adopted patch ids。
- adopted patch index 路径。
- created time。

## failure-index.json

失败索引必须包含：

- `schema_version`。
- `profile`。
- `run_id`。
- 失败阶段。
- 失败包或失败目标。
- 失败日志路径。
- 诊断上下文路径。
- health report 路径。
- resolved config 路径。
- worktree manifest 路径。
- checkpoint 索引路径。
- AI 修复历史路径。
- AI repair diff 列表路径。
- 最后现场摘要路径。
- created time。

## 崩溃恢复

- artifact staging 目录可由后续清理命令删除，但不得自动提升为成功产物。
- adopted patch staging 目录可由后续清理命令删除，除非 success lock 已引用该 patch id。
- run record 没有 final status 时，恢复流程必须先按 Run Record 规格标记为 `blocked`，再允许清理 staging。
- success lock 引用不存在的 artifact 或 adopted patch 时，文档静态检查必须报告一致性错误。

## 验收

- 成功 run 可以通过 `artifact-index.json` 找到固件、配置、日志、版本和 Docker 摘要。
- 失败 run 可以通过 `failure-index.json` 找到失败阶段、诊断上下文和 AI 修复历史。
- 失败 run 不更新 `locks/<profile>/success-lock.json`。
- AI 修复失败不生成 adopted patch。
- success lock 写入失败时，run final status 不是 `succeeded`。
- `logs --latest` 不展示 artifact staging 目录。

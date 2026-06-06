---
status: accepted
owner: engineering
last_updated: 2026-06-06
depends_on:
  - docs/03-specs/run-record-state-spec.md
  - docs/03-specs/source-plugin-spec.md
  - docs/04-decisions/ADR-0003-ai-repair-checkpoint.md
  - docs/04-decisions/ADR-0004-run-worktree-isolation.md
  - docs/04-decisions/ADR-0005-ai-repair-auto-adoption.md
  - docs/02-architecture/build-pipeline.md
---

# AI 修复规格

## 触发条件

构建失败后，如果 resolved config 中 `ai_repair.enabled: true`，系统生成诊断上下文并调用外部 AI CLI。

不触发 AI 修复的情况：

- AI repair 未启用。
- 预检或构建上下文校验已阻断，且阻断原因是 AI CLI 本身不可用。
- 已达到 `ai_repair.max_retries`。
- 失败发生在无法生成诊断上下文的阶段。

不触发时，run 仍必须归档失败现场；如果失败现场无法归档，run final status 为 `blocked`。

## 调用协议

AI CLI 由用户配置：

- `ai_repair.command`：可执行文件名或绝对路径。
- `ai_repair.args`：参数列表，支持占位符 `{context_file}`、`{worktree}`、`{run_id}`、`{profile}`、`{round}`。
- `ai_repair.timeout`：单轮修复超时时间，默认 `30m`。

调用规则：

- 工作目录必须是当前 run 工作树的宿主可访问源码路径。
- v1 不支持在 `docker-volume` storage driver 下调用外部 AI CLI；启用 AI 修复时，健康检查必须先确认 storage driver 为 `host-path` 或 `linux-path`。
- 如果 `args` 为空，系统把 `{context_file}` 作为唯一参数传给 command。
- 系统必须设置环境变量：`AUTO_OPENWRT_CONTEXT_FILE`、`AUTO_OPENWRT_WORKTREE`、`AUTO_OPENWRT_RUN_ID`、`AUTO_OPENWRT_PROFILE`、`AUTO_OPENWRT_REPAIR_ROUND`。
- stdout 写入 `diagnostics/<profile>/<run-id>/ai/round-<n>/stdout.log`。
- stderr 写入 `diagnostics/<profile>/<run-id>/ai/round-<n>/stderr.log`。
- 退出码、开始时间、结束时间和超时状态写入 AI 修复历史。
- AI CLI 返回非零退出码、超时或 diff 采集失败时，CLI 返回退出码 `7`，除非后续归档失败导致更高优先级的退出码 `8`。

## 修复上下文

诊断上下文路径：

```text
diagnostics/<profile>/<run-id>/ai/round-<n>/context.json
```

诊断上下文必须包含：

- 失败阶段。
- 失败包或失败目标。
- 关键日志片段路径。
- resolved config 路径。
- health report 路径。
- OpenWrt、feeds、插件源码版本。
- 插件风险类型。
- 当前 run 工作树差异摘要。
- worktree manifest 路径。
- 已应用的 adopted patch ids。
- 上一轮 AI 修复输出摘要。

## 检查点

每轮 AI 修复前必须创建当前 run 工作树检查点。检查点用于记录修复前源码状态和可回退边界。

检查点必须关联：

- checkpoint id。
- run id。
- profile。
- 修复轮次。
- 工作树路径。
- 修复前源码版本。
- 可回退材料路径。

检查点路径：

```text
checkpoints/<profile>/<run-id>/round-<n>/checkpoint.json
```

## 修改范围

AI CLI 允许修改当前 run 工作树，但不得直接修改共享源码缓存、success lock、历史 run record 或其他 profile 的工作树。所有修改必须关联到当前 run record，并记录差异。

v1 自动采纳范围：

- 只自动采纳可由 Git diff 表示的文本差异。
- 二进制文件、未跟踪大文件和工作树外路径变更必须进入诊断记录，但不得自动归档为 adopted patch。
- 如果 diff 采集失败，本轮修复视为失败，不得写入 success lock。
- diff 采集必须以当前 run 工作树为范围；工作树外路径变更只能记录到诊断上下文，不能进入 adopted patch。

## 重试规则

- 最多执行 `ai_repair.max_retries` 轮 AI 修复重试，默认 5。
- 每轮修复后从构建上下文校验阶段重新进入流水线。
- 成功后停止修复并归档产物。
- 超过次数后停止并保留失败现场。
- AI CLI 超时视为本轮失败，并计入重试次数。
- AI CLI 返回非零退出码时，本轮失败，不进入 Docker 构建重试。

## 自动采纳规则

- 修复后构建成功时，系统自动把最终 diff 归档为 profile 级 adopted patch。
- adopted patch 必须记录 patch id、来源 run id、来源修复轮次、diff 摘要和 patch 文件路径。
- success lock 必须记录 adopted patch ids。
- 修复失败或超过重试次数时，不得生成 adopted patch，不得更新 success lock。
- adopted patch 必须先通过产物规格中的 staging/finalize 流程写入，success lock 写入成功后 run final status 才能为 `succeeded`。

## 验收

- 构建失败后能生成 AI context.json。
- 每轮 AI 修复前创建 checkpoint。
- AI CLI stdout、stderr、退出码和耗时被记录。
- AI CLI 非零退出码不触发 Docker 重试。
- AI 修复成功后 adopted patch 和 success lock 能追溯来源 run 和修复轮次。

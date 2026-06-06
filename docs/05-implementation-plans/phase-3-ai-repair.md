---
status: accepted
owner: engineering
last_updated: 2026-06-06
depends_on:
  - docs/03-specs/ai-repair-spec.md
  - docs/03-specs/artifact-spec.md
  - docs/03-specs/run-record-state-spec.md
  - docs/03-specs/source-plugin-spec.md
  - docs/04-decisions/ADR-0003-ai-repair-checkpoint.md
  - docs/04-decisions/ADR-0005-ai-repair-auto-adoption.md
---

# Phase 3：AI 修复闭环

## 目标

实现构建失败后的诊断上下文生成、AI CLI 调用、当前 run 工作树检查点记录、最多 5 次自动修复重试，以及成功修复后的 adopted patch 自动采纳。

本计划依赖的工程规格已为 `accepted`，可以作为 Phase 3 代码实现准入。

## 交付

- 失败诊断上下文包。
- AI CLI 调用适配。
- 修复前检查点。
- AI stdout/stderr/exit code/timeout 记录。
- 修复差异记录。
- 修复重试控制。
- adopted patch 归档。
- 修复历史归档。
- adopted patch staging/finalize 与 success lock 写入顺序。

## 边界

- AI CLI 由用户安装、登录和授权。
- Auto-OpenWrt 不解释 AI 输出语义，只记录调用结果并采集当前 run 工作树差异。
- v1 只自动采纳可由 Git diff 表示的文本差异。
- AI 修复失败不更新 success lock，不生成 adopted patch。

## 验收

- 构建失败后可以生成诊断上下文。
- AI 修复前创建当前 run 工作树检查点。
- 每轮修复都记录输入、输出、差异和结果。
- AI CLI 非零退出码或超时被记录，并计入失败轮次。
- 最多重试 5 次。
- 修复后构建成功时自动生成 adopted patch，并写入 success lock。
- 修复失败后保留最后现场和全部修复历史，不生成 adopted patch，不更新 success lock。
- adopted patch 或 success lock finalize 失败时，run final status 不得为 `succeeded`。

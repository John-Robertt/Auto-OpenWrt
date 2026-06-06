---
status: accepted
owner: architecture
last_updated: 2026-06-06
depends_on:
  - docs/01-product/product-planning.md
  - docs/04-decisions/ADR-0003-ai-repair-checkpoint.md
  - docs/04-decisions/ADR-0004-run-worktree-isolation.md
---

# ADR-0005：AI 修复成功后自动采纳为可审计补丁

## 背景

AI 修复在当前 run 工作树中修改源码。若修复后的构建成功，用户希望后续同 profile 构建能自动复用这次成功修复；但直接把 AI 修改写入共享源码基线会降低审计和回滚能力。

## 决策

AI 修复后的构建成功时，系统自动把最终源码差异归档为 profile 级 adopted patch，并把 adopted patch id 写入 success lock。后续同 profile 构建准备运行工作树时，必须按 success lock 记录应用这些 adopted patches。

AI 修复失败或超过重试次数时，不得生成 adopted patch，不得更新 success lock。

## 理由

- 自动采纳减少用户在成功修复后的额外操作。
- 以补丁形式采纳保留了完整审计、回滚和复用边界。
- Success Lock 记录 adopted patch ids 后，成功产物可以追溯到源码版本和 AI 修复差异。

## 影响

- Run Record 必须记录 AI repair diff 列表和 adoption result。
- Success Lock 必须记录 adopted patch ids。
- 产物归档必须包含 adopted patches 或其索引。
- 源码管理在准备同 profile 运行工作树时，需要应用 success lock 中的 adopted patches。


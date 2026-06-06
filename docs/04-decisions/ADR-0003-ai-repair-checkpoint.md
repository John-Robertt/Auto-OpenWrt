---
status: accepted
owner: architecture
last_updated: 2026-06-06
depends_on:
  - docs/01-product/product-planning.md
  - docs/04-decisions/ADR-0004-run-worktree-isolation.md
---

# ADR-0003：AI 修复前必须创建检查点

## 背景

产品允许外部 AI CLI 在构建失败后尝试修改当前 run 工作树，并最多重试 5 次。该能力可以提高自动化程度，但也会带来误改和多轮修改难以审计的风险。

## 决策

每轮 AI 修复前必须创建当前 run 工作树检查点，并记录修复输入、输出、源码差异和重试结果。

## 理由

- 检查点让自动修复具备可回退边界。
- 差异记录让用户能审计 AI 修改内容。
- 修复历史能帮助失败后继续人工定位。

## 影响

- AI 修复不得绕过当前 run 工作树检查点直接修改源码。
- 每次修复必须关联 run record。
- 超过 5 次重试后停止，保留最后现场和全部修复历史。

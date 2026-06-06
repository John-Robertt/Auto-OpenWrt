---
status: accepted
owner: architecture
last_updated: 2026-06-06
depends_on: []
---

# 文档状态与决策状态规范

## 文档状态块

每份关键文档顶部必须使用统一状态块：

```markdown
---
status: draft | proposed | accepted | superseded
owner: product | architecture | engineering
last_updated: YYYY-MM-DD
depends_on:
  - docs/...
---
```

`depends_on: []` 表示无依赖。非空依赖必须指向仓库中存在的文档文件。

## 文档状态

- `draft`：草案，只能讨论，不能作为开发依据。
- `proposed`：待评审方案，可以用于对齐设计，不能直接作为最终开发依据。
- `accepted`：已确认，可以作为开发输入。
- `superseded`：已被新文档替代，保留历史，不再作为依据。

## ADR 状态

ADR 使用以下状态：

- `proposed`：决策待确认。
- `accepted`：决策已确认，后续实现必须遵守。
- `deprecated`：决策仍有历史意义，但不推荐新功能继续使用。
- `superseded`：决策已被新的 ADR 替代。

## 状态提升规则

- `draft` 到 `proposed`：文档结构完整，关键问题已列出。
- `proposed` 到 `accepted`：关键取舍已确认，验收标准明确，依赖文档已存在且均为 `accepted`。
- `accepted` 到 `superseded`：必须说明替代文档或替代 ADR。
- 任何状态提升都必须同时更新 `last_updated`。

## accepted 准入

工程规格或实施计划提升为 `accepted` 前，必须满足：

- 目标、边界、输入、输出、错误和验收标准明确。
- 涉及文件落盘时，路径、格式、ID、状态枚举和写入规则明确。
- 涉及命令或接口时，参数、默认值、退出码或失败行为明确。
- 关键取舍已由 accepted ADR 或 accepted 架构文档覆盖。
- `depends_on` 全部指向存在文件，且依赖图无环。
- README 状态表与文档 front matter 一致。

不满足以上条件的工程规格和实施计划必须保持 `proposed`，不得作为代码实现准入。

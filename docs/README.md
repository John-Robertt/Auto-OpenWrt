---
status: accepted
owner: architecture
last_updated: 2026-06-06
depends_on: []
---

# Auto-OpenWrt 文档中心

`docs/` 是 Auto-OpenWrt 的决策中枢。任何代码开发前，必须先完成对应的产品目标、架构归属、规格说明和验收标准；代码实现只能落地已确认文档，不在实现阶段临时补产品或架构决策。

## 阅读顺序

1. `00-governance/`：先理解文档先于代码、状态和决策规则。
2. `01-product/`：确认产品目标、用户需求和验收标准。
3. `02-architecture/`：确认系统结构、模块边界、数据模型和主流程。
4. `03-specs/`：确认配置、CLI、工作区、健康检查、AI 修复和产物归档规格。
5. `04-decisions/`：查看关键架构取舍的 ADR。
6. `05-implementation-plans/`：从已确认文档拆解开发阶段，并按 `development-roadmap.md` 渐进执行。

## 目录结构

```text
docs/
  README.md
  00-governance/
  01-product/
  02-architecture/
  03-specs/
  04-decisions/
  05-implementation-plans/
```

## 当前文档状态

| 文档 | 状态 | 职责 |
| --- | --- | --- |
| `00-governance/docs-first.md` | accepted | 文档先于代码规则 |
| `00-governance/decision-status.md` | accepted | 文档状态、决策状态和实现准入规则 |
| `01-product/product-planning.md` | accepted | 产品定位、目标、职责和验收标准 |
| `01-product/requirements.md` | accepted | 产品需求清单 |
| `01-product/user-workflows.md` | accepted | 用户工作流 |
| `02-architecture/architecture-design.md` | accepted | 总体架构 |
| `02-architecture/module-boundaries.md` | accepted | 模块职责和依赖方向 |
| `02-architecture/data-model.md` | accepted | 核心数据模型与持久化契约索引 |
| `02-architecture/build-pipeline.md` | accepted | 构建流水线 |
| `03-specs/*.md` | accepted | 工程行为规格 |
| `04-decisions/*.md` | accepted | 已确认架构决策 |
| `05-implementation-plans/*.md` | accepted | 阶段实施计划 |

## 当前实现准入结论

产品目标、核心架构方向、ADR、工程规格和 Phase 计划已可作为 v1 代码实现依据。进入代码实现前，仍必须持续满足以下门禁：

- `03-specs/` 中目标功能依赖的规格保持 `accepted`。
- 对应 `05-implementation-plans/` 阶段计划保持 `accepted`。
- `depends_on` 全部指向存在文件，且文档依赖图无环。
- README 状态表与各文档 front matter 保持一致。

## 开发前置条件

- 目标功能必须能追溯到 `01-product/` 中的产品目标或需求。
- 目标功能必须能归属到 `02-architecture/` 中的模块或流程。
- 涉及用户输入、输出、状态、错误处理或文件产物的功能，必须有 `03-specs/` 中的 accepted 规格说明。
- 出现关键取舍时，必须在 `04-decisions/` 中有 accepted ADR。
- 开发任务必须来自 `05-implementation-plans/` 中的 accepted 阶段计划，不能在实现计划中新增未经确认的产品或架构决策。
- 开发执行顺序必须遵守 `05-implementation-plans/development-roadmap.md`，每个增量保留验证和回溯证据。

## 当前关键决策

- project root 可以包含多个配置文件；每个配置文件通过 `workspace_id` 映射到独立 `workspaces/<workspace-id>/` 状态目录。
- 不同源码输入通过 `source_set_id` 隔离源码缓存，相同源码输入可以复用 `sources/source-sets/<source-set-id>/`。
- 每次构建使用 `workspaces/<workspace-id>/worktrees/<build-id>/<run-id>/` 专属运行工作树，`sources/source-sets/<source-set-id>/` 只作为源码缓存。
- `workspaces/<workspace-id>/worktrees/<build-id>/<run-id>/` 是逻辑工作树标识；物理源码位置由 `host-path`、`docker-volume` 或 `linux-path` storage driver 决定，并写入 worktree manifest。
- Docker 执行器只挂载当前 run 工作树、缓存和产物目录，路径映射写入 run record。
- Docker image 只固定构建环境，不包含 OpenWrt、feeds 或 plugins 源码。
- AI 修复只允许修改当前 run 工作树，成功后自动归档为 `workspace_id/build_id` 级 adopted patch，并写入 success lock。
- 构建流水线是编排层，只通过接口调用配置、健康检查、源码、插件、执行器、诊断、AI 修复和产物记录模块。

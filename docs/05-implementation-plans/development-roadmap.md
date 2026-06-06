---
status: accepted
owner: engineering
last_updated: 2026-06-06
depends_on:
  - docs/00-governance/docs-first.md
  - docs/05-implementation-plans/phase-1-foundation.md
  - docs/05-implementation-plans/phase-2-build-pipeline.md
  - docs/05-implementation-plans/phase-3-ai-repair.md
---

# 渐进式开发路线

## 目标

本文件把 Phase 1、Phase 2、Phase 3 拆成可连续交付的开发增量。每个增量必须满足：

- 渐进式：每次只引入一个清晰能力边界，不跨阶段混做。
- 可验证：每个增量都有明确命令、测试或人工验收证据。
- 可回溯：每个增量都能追溯到产品目标、架构边界、工程规格和 Phase 计划。

代码开发不得跳过前置增量。若实现过程中发现规格不足，先暂停实现并更新相关 `03-specs/` 或 ADR，再继续开发。

## 开发记录格式

每个开发增量完成时，必须在提交说明、PR 描述或阶段验收记录中包含：

```text
increment_id:
goal:
upstream_docs:
changed_modules:
created_artifacts:
verification:
known_limits:
```

字段规则：

- `increment_id` 使用本文件中的增量编号。
- `upstream_docs` 至少引用一个 Phase 计划和一个工程规格。
- `changed_modules` 只列本增量直接修改的模块。
- `created_artifacts` 记录新增命令、文件契约、测试样例或运行产物。
- `verification` 写明实际执行的命令和结果。
- `known_limits` 写明仍属于后续增量的边界。

## 增量路线

| 增量 | 目标 | 主要依据 | 交付物 | 验收门禁 |
| --- | --- | --- | --- | --- |
| D0 | 建立工程骨架与文档检查 | Phase 1、CLI 规格 | Go module、CLI app skeleton、文档静态检查脚本 | 文档检查通过；CLI 可输出 help；`go test ./...` 通过 |
| D1 | Workspace 与配置解析 | Phase 1、配置规格、工作区规格 | `init`、目录创建、示例配置、YAML parser、resolved config | `init` 幂等；配置错误包含字段路径和建议；resolved config 显式默认值 |
| D2 | Run Record 与 doctor | Phase 1、Run Record 规格、健康检查规格 | run id、doctor run、health report、run recovery | `doctor --json` 输出统一结构；中断 run 可恢复为 `blocked` |
| D3 | 共享源码与插件更新 | Phase 2、源码与插件规格 | `update`、clone/fetch/reset、source snapshot、插件风险识别 | update 成功记录 commit；失败返回仓库名称和退出码 `4` |
| D4 | 运行工作树与构建上下文 | Phase 2、工作区规格、健康检查规格 | worktree manifest、storage driver、adopted patch 应用入口、context check | manifest 记录 logical id 和物理位置；无效 target/plugin 阻断构建 |
| D5 | Docker 构建执行 | Phase 2、Docker 执行器规格 | Docker mount、构建命令、Docker 环境摘要、构建日志 | 不挂载 workspace root；Docker 启动失败返回 `5`；OpenWrt 构建失败返回 `6` |
| D6 | 产物与失败诊断 | Phase 2、产物规格 | artifact staging/finalize、failure-index、logs | success lock 失败时 run 不为 `succeeded`；`logs --latest` 不展示 staging |
| D7 | AI 修复调用闭环 | Phase 3、AI 修复规格 | AI context、checkpoint、CLI 调用、stdout/stderr/exit code、diff | 非零退出码或超时计入失败轮次；最多重试 5 次 |
| D8 | Adopted Patch 与端到端回归 | Phase 3、产物规格、源码与插件规格 | adopted patch finalize、success lock patch ids、多 profile 回归 | 成功修复可追溯 patch/run/round；失败不更新 success lock |

## 验证证据

每个增量至少保留以下证据：

- 单元测试或集成测试命令与结果。
- CLI JSON 输出样例，覆盖成功和失败路径。
- 生成的 run record、health report、resolved config、manifest、artifact index 或 failure index 路径。
- 与本增量相关的退出码验证。
- 如涉及持久化，必须验证原子写入、final status 和 `logs --latest` 可见性。

建议验证命令：

```sh
go test ./...
auto-openwrt init --workspace <tmp-workspace> --json
auto-openwrt doctor --workspace <tmp-workspace> --config <tmp-workspace>/config/auto-openwrt.yaml --json
auto-openwrt logs --workspace <tmp-workspace> --latest --json
```

具体命令可以随工程结构调整，但必须保持每个增量的验收语义不变。

## 回溯矩阵

| 能力 | 产品依据 | 架构依据 | 规格依据 | Phase |
| --- | --- | --- | --- | --- |
| CLI 与工作区初始化 | 产品策划、用户工作流 | 架构设计、模块边界 | CLI、配置、工作区 | Phase 1 |
| 健康检查 | 产品需求、用户工作流 | 构建流水线 | 健康检查、Run Record | Phase 1 |
| 源码与插件更新 | 产品需求 | 架构设计、构建流水线 | 源码与插件、配置 | Phase 2 |
| 运行工作树隔离 | 产品策划 | ADR-0004、数据模型 | 工作区、源码与插件 | Phase 2 |
| Docker 构建 | 产品策划 | ADR-0002、模块边界 | Docker 执行器、CLI | Phase 2 |
| 产物归档 | 产品需求、用户工作流 | 数据模型 | 产物、Run Record | Phase 2 |
| AI 修复 | 产品策划 | ADR-0003、ADR-0005 | AI 修复、产物、源码与插件 | Phase 3 |
| adopted patch 复用 | 产品需求 | ADR-0005、数据模型 | 源码与插件、产物 | Phase 3 |

## 暂停与回退规则

出现以下任一情况时，暂停代码实现：

- 实现者需要自行决定未在规格中定义的数据格式、路径、状态、退出码或错误语义。
- 单个增量开始引入后续增量的核心能力。
- 测试需要依赖未记录的外部状态或人工隐含步骤。
- 发现现有架构边界会导致跨模块双向依赖。

暂停后处理顺序：

1. 标记当前增量为 blocked。
2. 回到相关 `03-specs/` 或 ADR 补充决策。
3. 重新运行文档静态检查。
4. 再恢复代码实现。

## 完成标准

v1 开发完成必须同时满足：

- D0 到 D8 全部完成并有验证证据。
- 每个 public CLI 行为都有成功和失败路径测试。
- 每个持久化文件都有 schema、路径、写入时机和失败语义测试。
- 每个 Phase 的验收项均可由测试、运行记录或人工验收证据证明。
- 最新一次成功构建能追溯到配置、源码版本、health report、Docker 摘要、产物、success lock 和 adopted patch。

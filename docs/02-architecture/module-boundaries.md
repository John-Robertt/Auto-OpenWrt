---
status: accepted
owner: architecture
last_updated: 2026-06-06
depends_on:
  - docs/02-architecture/architecture-design.md
  - docs/04-decisions/ADR-0004-run-worktree-isolation.md
  - docs/04-decisions/ADR-0005-ai-repair-auto-adoption.md
  - docs/04-decisions/ADR-0006-source-set-config-isolation.md
---

# 模块边界

## 依赖方向

构建流水线是应用层编排者。CLI 只负责接收命令和展示结果；流水线通过接口调用各能力模块；能力模块不得反向依赖 CLI 或流水线的具体实现。

```text
CLI -> Build Application / Pipeline

Build Application / Pipeline -> Workspace Store
Build Application / Pipeline -> Config/Build Resolver
Build Application / Pipeline -> Health Checker
Build Application / Pipeline -> Source Manager
Build Application / Pipeline -> Plugin Manager
Build Application / Pipeline -> Docker Executor
Build Application / Pipeline -> Failure Diagnostics
Build Application / Pipeline -> AI Repair Coordinator
Build Application / Pipeline -> Artifact Recorder
```

允许的底层依赖：

- 各模块可以依赖共享的数据模型和文件契约。
- 各模块可以通过 Workspace Store 读写自己职责范围内的持久化文件。
- Docker Executor 只接收已解析的执行请求，不读取用户配置做决策。
- AI Repair Coordinator 只接收诊断上下文、当前 run 工作树和检查点信息，不直接更新 success lock。

## 模块职责

| 模块 | 职责 | 不负责 |
| --- | --- | --- |
| CLI | 接收用户命令，解析通用参数，展示结果和错误 | 直接执行 OpenWrt 内部构建细节 |
| Build Application / Pipeline | 编排一次 build/update/doctor/logs 工作流和阶段状态 | 保存不属于当前阶段的模块内部状态 |
| Workspace Store | 管理 project root、工作区状态目录、原子写入、run record、manifest、lock、artifact index | 判断 target 或插件兼容性 |
| Config/Build Resolver | 解析 user config，确定 `workspace_id`、`build_id` 和 `source_set_id`，生成 resolved config，展开默认值 | 修改源码或采纳 AI 补丁 |
| Health Checker | 执行预检和构建上下文校验，生成 Health Report | 代替构建失败诊断 |
| Source Manager | 更新 source-set 源码缓存，创建运行工作树，应用同 `workspace_id/build_id` adopted patches | 决定是否重试构建 |
| Plugin Manager | 接入 feeds/plugins，标记风险类型，提供插件上下文 | 修复插件源码 |
| Docker Executor | 隔离并执行当前 run 工作树中的构建命令 | 管理产品状态或 success lock |
| Failure Diagnostics | 收集失败阶段、日志、源码版本、插件风险和工作树差异 | 修改源码 |
| AI Repair Coordinator | 调用外部 AI CLI，记录输入、输出、退出码和差异 | 绕过检查点直接改工作区 |
| Artifact Recorder | 归档成功/失败产物，成功后写入 success lock 和 artifact index | 决定构建策略 |

## 边界规则

- 项目根目录是外层文件边界；工作区是配置文件对应的持久状态边界，所有构建记录、产物、诊断、运行工作树、检查点和 adopted patches 必须归属到 `workspaces/<workspace-id>/`。
- `workspace_id/build_id` 是长期状态隔离边界，不同配置文件的同名 build 不得共享 success lock、adopted patches、产物、诊断或 run record。
- `sources/source-sets/<source-set-id>/` 是源码缓存，不是构建时的可变源码区。
- `workspaces/<workspace-id>/worktrees/<build-id>/<run-id>/` 是当前 run 的唯一可变源码区。
- Build 是同一 workspace 内的固件构建目标隔离边界，不同 `workspace_id/build_id` 的产物、日志、运行工作树和 adopted patches 不得混淆。
- Docker 是 OpenWrt 构建环境边界，宿主 CLI 负责编排，容器负责执行。
- Docker Executor 只能挂载当前 run 工作树、缓存和产物目录，映射必须写入 run record。
- AI 修复必须通过当前 run 工作树检查点进入工作区，不允许无记录修改。
- AI 修复成功后的差异只能以 adopted patch 形式自动采纳。
- Success Lock 只能由成功构建流程写入；失败构建和失败修复不得更新 Success Lock。

## 接口隔离要求

- CLI 到流水线的输入必须是命令参数、project root 和 config path，不传递未解析的全局状态。
- 流水线到模块的输入必须是 resolved config、`workspace_id`、`build_id`、`source_set_id`、run id、路径和阶段上下文。
- 模块输出必须返回结构化结果：状态、产物路径、错误原因、下一步建议。
- 任一模块替换实现时，调用方不应修改自身控制流，只调整接口适配层。

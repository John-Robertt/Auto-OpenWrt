---
status: accepted
owner: engineering
last_updated: 2026-06-07
depends_on:
  - docs/02-architecture/build-pipeline.md
  - docs/02-architecture/data-model.md
  - docs/04-decisions/ADR-0006-source-set-config-isolation.md
---

# Run Record 与状态规格

## 职责边界

Run Record 是一次 build、doctor 或 update 的状态事实来源。Build Application / Pipeline 创建和更新 run record；能力模块只能返回结构化结果，不直接改写 run record。

Build run 必须归属到具体 `workspace_id/build_id`，并记录本次 resolved config 的 `source_set_id`。Doctor 和 update run 可以不关联 build，但如果命令指定了 `--config` 或 `--build`，run record 必须记录解析出的 `workspace_id` 或 `build_id`。

`update --build` 只涉及一个 source-set 时，update run 顶层必须记录 `source_set_id`。`update` 未指定 `--build` 且涉及多个 source-set 时，顶层 `source_set_id` 可以为空，但必须通过 `source-update-summary.json` 记录本次 source-set 集合。

## Pre-run Bootstrap

`build`、`update` 和绑定配置文件或 build 的 `doctor` 在创建正式 run record 前，必须先用命令参数和 user config 执行最小配置解析。

pre-run bootstrap 必须确定：

- `workspace_id`。
- 目标 `build_id`，仅 `build` 和指定 `--build` 的命令必填。
- 本次命令涉及的 `source_set_id` 或 source-set 集合。
- 配置 schema、build id、feed/plugin 引用和 enabled 状态均有效。

pre-run bootstrap 不写 run record、不写 Health Report。配置文件读取失败、YAML 语法错误、schema 错误、build 不存在、build 引用不存在或 disabled feed/plugin 时，CLI 必须返回退出码 `2`。

## Run ID

`run_id` 格式固定为：

```text
YYYYMMDDTHHMMSSZ-<6位小写字母或数字>
```

生成规则：

- 时间使用 UTC。
- 后缀由系统随机生成。
- 同一 project root 中不得复用 run id。

## Build 阶段 ID

build run 使用固定 stage id：

| 顺序 | stage id | 对应阶段 |
| --- | --- | --- |
| 1 | `run.create` | 创建 run record |
| 2 | `config.read` | 记录 pre-run bootstrap 使用的 user config 快照 |
| 3 | `config.resolve` | 解析 build / 写入 resolved config |
| 4 | `health.preflight` | 执行预检 |
| 5 | `source.update` | 更新 source-set 源码缓存 |
| 6 | `worktree.prepare` | 准备运行工作树 / 应用 adopted patches |
| 7 | `plugins.attach` | feeds / plugins 接入 |
| 8 | `health.build_context` | 构建上下文校验 |
| 9 | `build.config` | 生成 OpenWrt 构建配置 |
| 10 | `docker.build` | Docker 构建 |
| 11 | `build.result` | 判定构建结果 |
| 12 | `failure.diagnose` | 失败诊断 |
| 13 | `ai.repair` | AI 修复重试 |
| 14 | `patch.adopt` | adopted patch 归档 |
| 15 | `artifact.archive` | 归档成功产物或失败现场 |

阶段状态允许值：`pending`、`running`、`succeeded`、`failed`、`skipped`。

## Update 阶段 ID

update run 使用固定 stage id：

| 顺序 | stage id | 对应阶段 |
| --- | --- | --- |
| 1 | `run.create` | 创建 run record |
| 2 | `config.read` | 记录 pre-run bootstrap 使用的 user config 快照 |
| 3 | `config.resolve` | 解析 workspace、build 和 source-set 集合 |
| 4 | `source.update` | 更新 source-set 源码缓存并写入版本摘要 |

## 最终状态

最终状态允许值：

- `succeeded`：构建成功，artifact index、success lock 和 run final status 已全部写入。
- `failed`：构建或 AI 修复失败，失败现场已归档。
- `blocked`：配置、健康检查、工作树准备、补丁应用、归档恢复或人工中断导致流程不能继续。

最终状态只能写入一次。已经存在最终状态的 run record 不得再次改写最终状态。

## 写入协议

创建规则：

- `build` 在 pre-run bootstrap 成功后创建 `workspaces/<workspace-id>/runs/<build-id>/<run-id>/run.json`。
- 未绑定配置文件的 `doctor` 创建 `runs/doctor/<run-id>/run.json`。
- 绑定配置文件或 build 的 `doctor` 在 pre-run bootstrap 成功后创建 `workspaces/<workspace-id>/runs/doctor/<run-id>/run.json`。
- `update` 在 pre-run bootstrap 成功后创建 `workspaces/<workspace-id>/runs/update/<run-id>/run.json`。
- 创建 run record 时同时创建 `run.lock`，记录当前进程信息、开始时间和 command。

`config.read` 和 `config.resolve` 阶段必须基于 pre-run bootstrap 使用的同一份配置快照执行，不得重新读取另一份配置内容。

更新规则：

- 每次更新必须写入同目录临时文件，再原子 rename 为 `run.json`。
- 阶段开始时写 `running` 和 start time。
- 阶段结束时写 end time、status、result paths、error 和 suggestion。
- 阶段失败后，后续未执行阶段保持 `pending` 或写为 `skipped`。
- run final status 写入后必须删除 `run.lock`。

update run 成功时，run record paths 必须包含 `source_update_summary`。仅涉及一个 source-set 时，paths 还应包含 `source_set_snapshot`。

## 崩溃恢复

任一 mutating command 启动前，Pipeline 必须扫描未完成 run：

- 存在 `run.lock` 且进程仍存活：保持原状态。
- 存在 `run.lock` 但进程已不存在：把 run final status 写为 `blocked`，reason 为 `interrupted`。
- 不存在 final status 且不存在 `run.lock`：把 run final status 写为 `blocked`，reason 为 `incomplete-run-record`。

`logs --latest` 只选择已有 final status 的 run。没有 final status 的 run 不得被展示为成功或失败 latest。

## 错误对象

阶段错误对象固定为：

```json
{
  "code": "string",
  "message": "string",
  "suggestion": "string",
  "details": {}
}
```

规则：

- `message` 面向用户，必须说明失败原因。
- `suggestion` 必须给出下一步动作。
- `details` 只能包含结构化数据，不写大段日志。
- 大段日志通过路径引用。

## 验收

- build run 的 stage id 与构建流水线阶段一一对应。
- 最终状态只能写入一次。
- 运行中断后，下一次 mutating command 会把未完成 run 标记为 `blocked`。
- `logs --latest` 不会把未完成 run 当作成功记录。
- 每个失败阶段都有结构化 error 和日志路径。

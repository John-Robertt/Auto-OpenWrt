---
status: accepted
owner: engineering
last_updated: 2026-06-06
depends_on:
  - docs/02-architecture/build-pipeline.md
  - docs/02-architecture/data-model.md
---

# Run Record 与状态规格

## 职责边界

Run Record 是一次 build、doctor 或 update 的状态事实来源。Build Application / Pipeline 创建和更新 run record；能力模块只能返回结构化结果，不直接改写 run record。

## Run ID

`run_id` 格式固定为：

```text
YYYYMMDDTHHMMSSZ-<6位小写字母或数字>
```

生成规则：

- 时间使用 UTC。
- 后缀由系统随机生成。
- 同一 workspace 中不得复用 run id。

## Build 阶段 ID

build run 使用固定 stage id：

| 顺序 | stage id | 对应阶段 |
| --- | --- | --- |
| 1 | `run.create` | 创建 run record |
| 2 | `config.read` | 读取 user config |
| 3 | `config.resolve` | 解析 profile / 写入 resolved config |
| 4 | `health.preflight` | 执行预检 |
| 5 | `source.update` | 更新共享源码缓存 |
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

## 最终状态

最终状态允许值：

- `succeeded`：构建成功，artifact index、success lock 和 run final status 已全部写入。
- `failed`：构建或 AI 修复失败，失败现场已归档。
- `blocked`：配置、健康检查、工作树准备、补丁应用、归档恢复或人工中断导致流程不能继续。

最终状态只能写入一次。已经存在最终状态的 run record 不得再次改写最终状态。

## 写入协议

创建规则：

- `build` 在读取配置前创建 `runs/<profile>/<run-id>/run.json`。
- `doctor` 创建 `runs/doctor/<run-id>/run.json`。
- `update` 创建 `runs/update/<run-id>/run.json`。
- 创建 run record 时同时创建 `run.lock`，记录当前进程信息、开始时间和 command。

更新规则：

- 每次更新必须写入同目录临时文件，再原子 rename 为 `run.json`。
- 阶段开始时写 `running` 和 start time。
- 阶段结束时写 end time、status、result paths、error 和 suggestion。
- 阶段失败后，后续未执行阶段保持 `pending` 或写为 `skipped`。
- run final status 写入后必须删除 `run.lock`。

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

---
status: accepted
owner: engineering
last_updated: 2026-06-07
depends_on:
  - docs/02-architecture/architecture-design.md
  - docs/02-architecture/build-pipeline.md
  - docs/03-specs/ai-repair-spec.md
  - docs/03-specs/artifact-spec.md
  - docs/03-specs/config-spec.md
  - docs/03-specs/docker-executor-spec.md
  - docs/03-specs/health-check-spec.md
  - docs/03-specs/run-record-state-spec.md
  - docs/03-specs/source-plugin-spec.md
  - docs/03-specs/workspace-spec.md
---

# CLI 规格

## 命令边界

Auto-OpenWrt v1 提供以下 CLI 能力：

- `init`：创建项目根目录骨架与示例配置。
- `doctor`：执行健康检查。
- `build`：构建指定 build。
- `update`：更新 OpenWrt、feeds 和 plugins 的 source-set 源码缓存。
- `logs`：查看构建日志、诊断记录和 adopted patch 记录。

## 通用行为

命令格式：

```text
auto-openwrt <command> [flags]
```

通用参数：

- `--project <path>`：指定 project root；默认当前目录。
- `--config <path>`：指定 user config；默认 `<project>/configs/auto-openwrt.yaml`。
- `--json`：输出机器可读 JSON。
- `--verbose`：输出详细日志。

通用退出码：

- `0`：成功。
- `2`：参数、配置文件读取、YAML 语法或配置 schema 错误。
- `3`：已生成 Health Report 的健康检查阻断。
- `4`：源码缓存更新、运行工作树准备或 adopted patch 应用失败。
- `5`：Docker 容器启动或执行环境失败。
- `6`：OpenWrt 构建命令失败。
- `7`：AI 修复失败、超时或超过重试次数。
- `8`：工作区读写、run record 更新、产物归档或 success lock 写入失败。

退出码优先级：

1. 参数或配置解析失败返回 `2`。
2. 已生成 Health Report 且存在阻断项返回 `3`。
3. 进入源码或工作树阶段后失败返回 `4`。
4. Docker 容器无法启动返回 `5`。
5. Docker 容器内 OpenWrt 构建命令失败返回 `6`。
6. AI 修复流程失败返回 `7`。
7. 持久化写入或归档失败返回 `8`。

通用规则：

- 命令失败时必须返回非零退出码。
- 用户可读错误必须包含失败原因和下一步建议。
- `build`、`update` 和绑定配置文件或 build 的 `doctor` 必须先完成 pre-run bootstrap，解析出 run record 所需的 `workspace_id`、`build_id` 和 source-set 信息，再创建正式 run record。
- pre-run bootstrap 中的参数、配置文件读取、YAML、schema、build id 或 feed/plugin 引用错误返回 `2`，不创建 run record，也不生成 Health Report。
- `doctor` 和 `update` 在 pre-run bootstrap 成功后必须生成独立 run record。
- 宿主 CLI 不直接执行 OpenWrt 内部构建命令；OpenWrt 构建命令只能由 Docker Executor 在当前 run 工作树中执行。

## JSON 输出

`--json` 输出统一结构：

```json
{
  "schema_version": 1,
  "command": "build",
  "status": "succeeded",
  "project_root": "/abs/project",
  "workspace_id": "auto-openwrt",
  "source_set_id": "src-123456789abc",
  "build_id": "x86-64",
  "run_id": "20260606T120000Z-abc123",
  "paths": {},
  "error": null
}
```

字段规则：

- `schema_version` 固定为 `1`。
- `command` 为实际命令名。
- `status` 允许值：`succeeded`、`failed`、`blocked`。
- `project_root` 必须为绝对路径。
- `workspace_id` 对已解析配置的 `build`、`doctor`、`update` 和 `logs` 输出必填；`init` 可为空。
- `source_set_id` 对已解析单个 build 源码输入的 `build` 和 `update --build` 输出必填；`update` 未指定 `--build` 且涉及多个 source-set 时为空，并通过 `paths.source_update_summary` 记录 source-set 集合。
- `build_id` 对 `build` 必填，对 `doctor` 和 `update` 可为空。
- `run_id` 对成功创建 run record 的命令必填。
- `paths` 只放结构化路径索引，如 run record、health report、artifact index、failure index。
- `error` 成功时为 `null`，失败时使用固定错误对象。

错误对象：

```json
{
  "code": "CONFIG_PARSE_ERROR",
  "message": "配置文件无法解析",
  "suggestion": "检查 YAML 语法并重新运行命令",
  "details": {}
}
```

## `init`

命令格式：

```text
auto-openwrt init [--project <path>] [--force] [--json]
```

职责：

- 创建 project root 目录结构。
- 创建示例配置 `configs/auto-openwrt.yaml`。
- 输出下一步建议。

规则：

- 目录已存在时保持幂等。
- 配置文件已存在时默认失败并返回退出码 `2`。
- 传入 `--force` 时允许覆盖示例配置，但不得删除 `sources/`、`cache/`、`runs/` 或 `workspaces/`。
- `init` 不创建 run record。

验收：

- 空目录中执行后，必需目录全部存在。
- 重复执行不破坏已有运行记录。
- 已有配置且未传 `--force` 时，返回可读错误。

## `doctor`

命令格式：

```text
auto-openwrt doctor [--project <path>] [--config <path>] [--build <id>] [--json]
```

职责：

- 执行 pre-run bootstrap。
- 创建 doctor run record。
- 独立执行预检。
- 可选读取 build 并检查配置声明完整性。
- 输出系统环境、Docker、AI CLI、权限、磁盘、网络、project root、工作区和运行工作树存储状态。

规则：

- 未指定 build 时，不创建 build 运行工作树。
- 未显式指定 `--config` 时，使用默认配置 `<project>/configs/auto-openwrt.yaml`，属于绑定配置文件的 doctor。
- 指定 build 时，只解析该 build 的配置引用，不更新 source-set 源码缓存。
- 参数、YAML schema、build id 或 feed/plugin 引用错误返回 `2`，不创建 run record。
- Health Report 已生成且存在 `fail` 项时返回 `3`。

验收：

- 关键失败项明确标记为 `fail`。
- 每个 `fail` 项都有修复建议。
- 未指定 build 时，不要求创建构建运行工作树。
- 指定 build 且配置引用不存在时，返回退出码 `2`，不创建 run record，也不生成 Health Report。

## `build`

命令格式：

```text
auto-openwrt build --build <id> [--project <path>] [--config <path>] [--json]
```

职责：

- 执行 pre-run bootstrap，读取 user config 并解析 build。
- 创建 run record。
- 生成 resolved config。
- 执行预检。
- 更新 source-set 源码缓存。
- 准备当前 run 工作树并应用 adopted patches。
- 接入 feeds 和 plugins。
- 执行构建上下文校验。
- 生成 OpenWrt 构建配置。
- 触发 Docker 构建。
- 归档产物、失败现场或 adopted patch。

规则：

- `--build` 必填，缺失时返回 `2`。
- build 不存在、build 引用不存在或 disabled feed/plugin 时返回 `2`，不创建 run record。
- 预检失败时阻断构建，不进入源码更新。
- 构建上下文校验失败时，不进入 Docker 构建。
- 构建失败时必须生成诊断上下文；无法生成诊断上下文时 run final status 为 `blocked`。
- AI 修复失败时不更新 success lock。

验收：

- 指定 build 能进入独立构建流程。
- 当前 run 工作树逻辑标识、物理位置和 storage driver 写入 run record。
- 预检失败时阻断构建。
- 构建失败时能生成诊断上下文。
- AI 修复成功后自动归档 adopted patch 并更新 success lock。
- AI 修复失败时不更新 success lock。

## `update`

命令格式：

```text
auto-openwrt update [--project <path>] [--config <path>] [--build <id>] [--json]
```

职责：

- 创建 update run record。
- 更新配置引用的 OpenWrt source-set 源码缓存。
- 更新配置中启用的 feeds 和 plugins source-set 源码缓存。
- 记录源码版本摘要。

规则：

- 指定 `--build` 时，只更新该 build 对应的 source-set cache。
- 未指定 `--build` 时，更新当前 config 中所有 build 引用的 source-set cache。
- 未指定 `--build` 且多个 build 解析到相同 source-set 时，只更新一次该 source-set cache。
- 未指定 `--build` 且涉及多个 source-set 时，命令仍只创建一个 update run record；JSON 顶层 `source_set_id` 为空，`paths.source_update_summary` 必须指向本次更新的 source-set 集合摘要。
- build 不存在、build 引用不存在或 disabled feed/plugin 时返回 `2`，不创建 run record。
- `update` 不创建构建运行工作树，不写 success lock，不生成固件产物。
- 任一仓库更新失败时返回 `4`，并在错误详情中包含仓库名称和 repo URL。

验收：

- OpenWrt 源码缓存可被 clone 或 fetch。
- feeds/plugins 更新失败时，返回可读错误和失败仓库名称。
- update 成功后，后续 build 可从对应 source-set cache 创建 run 工作树。

## `logs`

命令格式：

```text
auto-openwrt logs [--project <path>] [--config <path>] [--build <id>] [--run <run-id>] [--latest] [--json]
```

职责：

- 查看最近成功或失败的 final run record。
- 查看失败诊断、AI 修复历史、checkpoint 索引和 adopted patch 记录。

规则：

- `--run` 指定时读取指定 run。
- `--latest` 指定时读取最近 final run。
- 未指定 `--run` 或 `--latest` 时，默认等同 `--latest`。
- 指定 `--config` 时，只在该 `workspace_id` 下查找。
- 指定 build 时，只在该 build 下查找；如果未指定 `--config`，则在所有 `workspace_id` 中查找同名 build 的 final run。
- 没有 final status 的 run 不得被 `--latest` 选中。
- 指定 run 不存在时返回 `2`。

验收：

- 用户可以从任意成功产物追溯到 `workspace_id`、`source_set_id`、resolved config、源码版本、adopted patches、health report、构建日志和 Docker 环境摘要。
- 用户可以从任意失败 run 追溯到失败阶段、诊断上下文、AI 修复历史和最后现场摘要。
- `logs --latest` 不会把 artifact staging 或未完成 run 展示为成功产物。

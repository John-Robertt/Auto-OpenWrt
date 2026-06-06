---
status: accepted
owner: engineering
last_updated: 2026-06-06
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

- `init`：创建工作区与示例配置。
- `doctor`：执行健康检查。
- `build`：构建指定 build profile。
- `update`：更新 OpenWrt、feeds 和 plugins 共享源码缓存。
- `logs`：查看构建日志、诊断记录和 adopted patch 记录。

## 通用行为

命令格式：

```text
auto-openwrt <command> [flags]
```

通用参数：

- `--workspace <path>`：指定 workspace root；默认当前目录。
- `--config <path>`：指定 user config；默认 `<workspace>/config/auto-openwrt.yaml`。
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
- `build` 必须先生成 run record，再进入配置解析后的构建阶段。
- `doctor` 和 `update` 必须生成独立 run record。
- 宿主 CLI 不直接执行 OpenWrt 内部构建命令；OpenWrt 构建命令只能由 Docker Executor 在当前 run 工作树中执行。

## JSON 输出

`--json` 输出统一结构：

```json
{
  "schema_version": 1,
  "command": "build",
  "status": "succeeded",
  "workspace": "/abs/workspace",
  "profile": "x86-64",
  "run_id": "20260606T120000Z-abc123",
  "paths": {},
  "error": null
}
```

字段规则：

- `schema_version` 固定为 `1`。
- `command` 为实际命令名。
- `status` 允许值：`succeeded`、`failed`、`blocked`。
- `workspace` 必须为绝对路径。
- `profile` 对 `build` 必填，对 `doctor` 和 `update` 可为空。
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
auto-openwrt init [--workspace <path>] [--force] [--json]
```

职责：

- 创建 workspace 目录结构。
- 创建示例配置 `config/auto-openwrt.yaml`。
- 输出下一步建议。

规则：

- 目录已存在时保持幂等。
- 配置文件已存在时默认失败并返回退出码 `2`。
- 传入 `--force` 时允许覆盖示例配置，但不得删除 `sources/`、`runs/`、`artifacts/`、`diagnostics/`、`patches/` 或 `locks/`。
- `init` 不创建 run record。

验收：

- 空目录中执行后，必需目录全部存在。
- 重复执行不破坏已有运行记录。
- 已有配置且未传 `--force` 时，返回可读错误。

## `doctor`

命令格式：

```text
auto-openwrt doctor [--workspace <path>] [--config <path>] [--profile <name>] [--json]
```

职责：

- 创建 doctor run record。
- 独立执行预检。
- 可选读取 profile 并检查配置引用完整性。
- 输出系统环境、Docker、AI CLI、权限、磁盘、网络、工作区和运行工作树存储状态。

规则：

- 未指定 profile 时，不创建 build 运行工作树。
- 指定 profile 时，只解析该 profile 的配置引用，不更新共享源码缓存。
- 参数或 YAML schema 错误返回 `2`。
- Health Report 已生成且存在 `fail` 项时返回 `3`。

验收：

- 关键失败项明确标记为 `fail`。
- 每个 `fail` 项都有修复建议。
- 未指定 profile 时，不要求创建构建运行工作树。
- 指定 profile 且配置引用不存在时，返回退出码 `3`。

## `build`

命令格式：

```text
auto-openwrt build --profile <name> [--workspace <path>] [--config <path>] [--json]
```

职责：

- 创建 run record。
- 读取 user config。
- 解析 build profile。
- 生成 resolved config。
- 执行预检。
- 更新共享源码缓存。
- 准备当前 run 工作树并应用 adopted patches。
- 接入 feeds 和 plugins。
- 执行构建上下文校验。
- 生成 OpenWrt 构建配置。
- 触发 Docker 构建。
- 归档产物、失败现场或 adopted patch。

规则：

- `--profile` 必填，缺失时返回 `2`。
- 预检失败时阻断构建，不进入源码更新。
- 构建上下文校验失败时，不进入 Docker 构建。
- 构建失败时必须生成诊断上下文；无法生成诊断上下文时 run final status 为 `blocked`。
- AI 修复失败时不更新 success lock。

验收：

- 指定 profile 能进入独立构建流程。
- 当前 run 工作树逻辑标识、物理位置和 storage driver 写入 run record。
- 预检失败时阻断构建。
- 构建失败时能生成诊断上下文。
- AI 修复成功后自动归档 adopted patch 并更新 success lock。
- AI 修复失败时不更新 success lock。

## `update`

命令格式：

```text
auto-openwrt update [--workspace <path>] [--config <path>] [--profile <name>] [--json]
```

职责：

- 创建 update run record。
- 更新 OpenWrt 共享源码缓存。
- 更新配置中启用的 feeds 和 plugins 共享源码缓存。
- 记录源码版本摘要。

规则：

- 指定 `--profile` 时，只更新该 profile 引用的 feeds/plugins。
- 未指定 `--profile` 时，更新所有启用的 feeds/plugins。
- `update` 不创建构建运行工作树，不写 success lock，不生成固件产物。
- 任一仓库更新失败时返回 `4`，并在错误详情中包含仓库名称和 repo URL。

验收：

- OpenWrt 源码缓存可被 clone 或 fetch。
- feeds/plugins 更新失败时，返回可读错误和失败仓库名称。
- update 成功后，后续 build 可从共享源码缓存创建 run 工作树。

## `logs`

命令格式：

```text
auto-openwrt logs [--workspace <path>] [--profile <name>] [--run <run-id>] [--latest] [--json]
```

职责：

- 查看最近成功或失败的 final run record。
- 查看失败诊断、AI 修复历史、checkpoint 索引和 adopted patch 记录。

规则：

- `--run` 指定时读取指定 run。
- `--latest` 指定时读取最近 final run。
- 未指定 `--run` 或 `--latest` 时，默认等同 `--latest`。
- 指定 profile 时，只在该 profile 下查找。
- 没有 final status 的 run 不得被 `--latest` 选中。
- 指定 run 不存在时返回 `2`。

验收：

- 用户可以从任意成功产物追溯到 resolved config、源码版本、adopted patches、health report、构建日志和 Docker 环境摘要。
- 用户可以从任意失败 run 追溯到失败阶段、诊断上下文、AI 修复历史和最后现场摘要。
- `logs --latest` 不会把 artifact staging 或未完成 run 展示为成功产物。

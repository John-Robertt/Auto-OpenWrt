---
status: accepted
owner: engineering
last_updated: 2026-06-07
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
| D1a | Project Root / Workspace / Builds 命名与目录迁移 | ADR-0006、CLI 规格、配置规格、工作区规格 | `--project`、`--build`、`configs/`、`workspaces/<workspace-id>/`、`workspace.id`、`builds[]`、`build_id`、source-set cache 路径 | 旧 `--workspace`、`--profile`、`profiles[]` 目标表面不再作为 v1 主接口；多配置同名 build 的状态目录互不覆盖 |
| D2 | Run Record 与 doctor | Phase 1、Run Record 规格、健康检查规格 | run id、doctor run、health report、run recovery | `doctor --json` 输出统一结构；中断 run 可恢复为 `blocked` |
| D3 | Source-set 源码与插件更新 | Phase 2、源码与插件规格 | `update`、clone/fetch/reset、source-set snapshot、插件风险识别 | update 成功记录 commit；失败返回仓库名称和退出码 `4` |
| D4 | 运行工作树与构建上下文 | Phase 2、工作区规格、健康检查规格 | worktree manifest、storage driver、adopted patch 应用入口、context check | manifest 记录 logical id 和物理位置；无效 target/plugin 阻断构建 |
| D5 | Docker 构建执行 | Phase 2、Docker 执行器规格 | Docker mount、构建命令、Docker 环境摘要、构建日志 | 不挂载 project root；Docker 启动失败返回 `5`；OpenWrt 构建失败返回 `6` |
| D6 | 产物与失败诊断 | Phase 2、产物规格 | artifact staging/finalize、failure-index、logs | success lock 失败时 run 不为 `succeeded`；`logs --latest` 不展示 staging |
| D7 | AI 修复调用闭环 | Phase 3、AI 修复规格 | AI context、checkpoint、CLI 调用、stdout/stderr/exit code、diff | 非零退出码或超时计入失败轮次；最多重试 5 次 |
| D8 | Adopted Patch 与端到端回归 | Phase 3、产物规格、源码与插件规格 | adopted patch finalize、success lock patch ids、多 build 回归 | 成功修复可追溯 patch/run/round；失败不更新 success lock |

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
auto-openwrt init --project <tmp-project> --json
auto-openwrt doctor --project <tmp-project> --config <tmp-project>/configs/auto-openwrt.yaml --json
auto-openwrt logs --project <tmp-project> --latest --json
```

具体命令可以随工程结构调整，但必须保持每个增量的验收语义不变。

## 回溯矩阵

| 能力 | 产品依据 | 架构依据 | 规格依据 | Phase |
| --- | --- | --- | --- | --- |
| CLI 与 project root 初始化 | 产品策划、用户工作流 | 架构设计、模块边界 | CLI、配置、工作区 | Phase 1 |
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

## 增量验收记录

### D0：建立工程骨架与文档检查

```text
increment_id: D0
goal: 建立工程骨架与文档检查
upstream_docs: docs/05-implementation-plans/phase-1-foundation.md, docs/03-specs/cli-spec.md
changed_modules: cmd/auto-openwrt, cmd/doccheck, internal/cli, internal/docscheck
created_artifacts: Go module、CLI help skeleton、文档静态检查命令、CLI/doccheck 单元测试
verification: go test ./...；go run ./cmd/doccheck；go run ./cmd/auto-openwrt --help；go run ./cmd/auto-openwrt doctor --help
known_limits: init、doctor、build、update、logs 的业务行为仍按后续增量实现
```

验收结论：

- 文档静态检查覆盖 front matter、依赖存在性、依赖图无环和 README 状态一致性。
- CLI 可以输出根 help 和各命令 help。
- 非 help 的业务命令在 D0 返回退出码 `2`，不提前实现后续增量能力。

### D1：Workspace 与配置解析

```text
increment_id: D1
goal: Workspace 与配置解析
upstream_docs: docs/05-implementation-plans/phase-1-foundation.md, docs/03-specs/cli-spec.md, docs/03-specs/config-spec.md, docs/03-specs/workspace-spec.md
changed_modules: internal/cli, internal/app, internal/workspace, internal/config
created_artifacts: init 命令、workspace 目录创建、示例配置、YAML parser、resolved config 内部 API、配置解析/工作区/CLI 测试
verification: go test ./...；go run ./cmd/doccheck；go run ./cmd/auto-openwrt init --workspace /tmp/auto-openwrt-d1-final-98Zqrv --json；/tmp/auto-openwrt-d1-final-bin init --workspace /tmp/auto-openwrt-d1-final-98Zqrv --json 返回退出码 2 和 CONFIG_EXISTS
known_limits: doctor run、run id 生成、run record、health report 和中断恢复仍属于 D2；源码更新、运行工作树准备和构建流程不在 D1 范围
```

验收结论：

- `init --json` 可以创建完整 workspace 目录树和 `config/auto-openwrt.yaml` 示例配置。
- 已存在配置且未传 `--force` 时返回退出码 `2`，错误 code 为 `CONFIG_EXISTS`，不覆盖用户配置。
- `--force` 只覆盖示例配置，不删除已有 run、source、artifact、diagnostic、patch 或 lock 状态。
- 示例配置可解析，resolved config 内部 API 显式展开 `workspace.worktree_storage`、`docker.image`、`docker.platform`、`ai_repair.adoption: auto`、默认重试次数、默认 jobs 和空 adopted patch ids。
- 配置 schema 错误包含字段路径、原因和修复建议。

ADR-0006 后续调整：

- D1 验收记录反映 ADR-0006 前的已完成实现，`--workspace`、`config/auto-openwrt.yaml` 和顶层状态目录属于待迁移表面。
- D1a 必须在 D2 前完成，把 CLI、示例配置、目录创建、resolved config 路径和测试迁移到 `--project`、`--build`、`configs/`、`workspaces/<workspace-id>/`、`builds[]` 与 `build_id`。

### D1a：Project Root / Workspace / Builds 命名与目录迁移

```text
increment_id: D1a
goal: Project Root / Workspace / Builds 命名与目录迁移
upstream_docs: docs/05-implementation-plans/phase-1-foundation.md, docs/03-specs/cli-spec.md, docs/03-specs/config-spec.md, docs/03-specs/workspace-spec.md, docs/04-decisions/ADR-0006-source-set-config-isolation.md
changed_modules: internal/cli, internal/app, internal/workspace, internal/config
created_artifacts: --project/--build CLI 表面、project root 目录骨架、示例 workspace 状态目录、builds[] 示例配置、workspace_id/build_id/source_set_id resolved config、D1a CLI/config/workspace 测试
verification: go test ./...；go run ./cmd/doccheck；go run ./cmd/auto-openwrt --help；go run ./cmd/auto-openwrt doctor --help；临时二进制 init --project <tmp> --json；重复 init 返回退出码 2 和 CONFIG_EXISTS；init --workspace <tmp> 返回退出码 2
known_limits: doctor run、run id 生成、run record、health report、pre-run bootstrap 持久化和中断恢复仍属于 D2；源码更新、运行工作树准备和构建流程不在 D1a 范围
```

验收结论：

- CLI help 和 `init` 主接口已迁移到 `--project`；`--workspace`、`--profile` 不再作为 v1 主接口。
- `init --json` 可以创建 project root 骨架、`configs/auto-openwrt.yaml` 和示例 `workspaces/auto-openwrt/` 状态目录。
- 示例配置可解析为 `builds[]` 模型，resolved config 显式输出 `workspace_id`、`build_id`、`source_set_id`、project root、logical worktree id、默认值和空 adopted patch ids。
- 旧 `profiles[]` 配置返回 schema 错误，并提示迁移到 `builds[]`。
- 多配置同名 build 的状态目录通过 `workspaces/<workspace-id>/` 路径隔离，后续 run record 写入由 D2 实现。

### D2：Run Record 与 doctor

```text
increment_id: D2
goal: Run Record 与 doctor
upstream_docs: docs/05-implementation-plans/phase-1-foundation.md, docs/03-specs/cli-spec.md, docs/03-specs/health-check-spec.md, docs/03-specs/run-record-state-spec.md
changed_modules: internal/cli, internal/app, internal/health, internal/runrecord
created_artifacts: doctor run record、run id 生成、health-report.json、run.lock、未完成 run 恢复、logs final run 查询、D2 app/health/runrecord/CLI 测试
verification: go test ./...；go run ./cmd/doccheck；git diff --check；go run ./cmd/auto-openwrt doctor --project <tmp> --config <tmp>/configs/auto-openwrt.yaml --json 可生成 run record 与 Health Report
known_limits: source-set 更新、运行工作树准备、Docker 构建、产物归档和 AI 修复仍属于 D3-D8
```

验收结论：

- `doctor --json` 在 pre-run bootstrap 成功后创建 `workspaces/<workspace-id>/runs/doctor/<run-id>/run.json` 和 `health-report.json`。
- Health Report 覆盖宿主、Docker、project root、workspace、cache、磁盘和 AI CLI 预检；生成报告后存在 fail 项时返回退出码 `3`。
- 参数、配置读取、YAML、schema、build id 或 feed/plugin 引用错误返回退出码 `2`，不创建 run record，也不生成 Health Report。
- run record 写入 final status 后删除 `run.lock`；下一次 mutating command 会把中断或未完成 run 恢复为 `blocked`。
- `logs --latest` 只读取已有 final status 的 run，不选择未完成 run。

### D3：Source-set 源码与插件更新

```text
increment_id: D3
goal: Source-set 源码与插件更新
upstream_docs: docs/05-implementation-plans/phase-2-build-pipeline.md, docs/03-specs/source-plugin-spec.md, docs/03-specs/cli-spec.md, docs/03-specs/run-record-state-spec.md
changed_modules: internal/cli, internal/app, internal/config, internal/source, internal/runrecord
created_artifacts: update 命令实现、source-set 更新计划、Git clone/fetch/reset/clean Source Manager、source-set.json、source-update-summary.json、插件风险识别、D3 config/source/app/CLI 测试
verification: go test ./...；go run ./cmd/doccheck；git diff --check；本地临时 Git 仓库执行 go run ./cmd/auto-openwrt update --project <tmp> --build x86-64 --json 成功
known_limits: build 命令、运行工作树准备、feeds/plugins 接入到当前 run 工作树、adopted patch 应用、Docker 构建、产物归档和 failure-index 仍属于 D4-D6
```

验收结论：

- `update --build <id> --json` 可以创建 update run record，更新对应 source-set cache，并输出 `source_set_id`、`source_set_snapshot` 和 `source_update_summary`。
- `update` 未指定 `--build` 时使用单个 update run 覆盖当前配置中所有 build 引用的 source-set，并对相同 source-set 去重；多个 source-set 通过 `source-update-summary.json` 记录集合。
- OpenWrt、feeds 和 plugins cache 遵循 clone 或 fetch/checkout/reset/clean 更新规则，成功后 snapshot 记录 commit 和 `dirty_state: false`。
- 插件风险识别覆盖显式 risk、patch、`luci-app-*`、`KernelPackage/` 和 unknown 默认路径。
- 仓库更新失败返回退出码 `4`，错误详情包含仓库名称、repo URL、git command、exit code 和 stderr；配置错误仍返回 `2` 且不创建 run record。
- D3 不创建运行工作树，不写 success lock，不生成固件产物。

### D4：运行工作树与构建上下文

```text
increment_id: D4
goal: 运行工作树与构建上下文
upstream_docs: docs/05-implementation-plans/phase-2-build-pipeline.md, docs/03-specs/workspace-spec.md, docs/03-specs/source-plugin-spec.md, docs/03-specs/health-check-spec.md, docs/03-specs/run-record-state-spec.md
changed_modules: internal/cli, internal/app, internal/config, internal/source, internal/health
created_artifacts: build 命令 D4 流水线入口、worktree manifest、host-path/linux-path/docker-volume storage driver 准备入口、adopted patch 应用入口、feeds.conf/plugin attach summary、构建上下文校验、D4 app/source/health/CLI 测试
verification: go test ./...；go run ./cmd/doccheck；git diff --check
known_limits: D4 在 health.build_context 通过后以 DOCKER_BUILD_NOT_IMPLEMENTED 临时 blocked 停在 D5 边界；OpenWrt 构建配置生成、Docker 构建、产物归档、failure-index、success lock 写入和 AI 修复仍属于 D5-D8
```

验收结论：

- `build --build <id>` 已接入 D4 前半段流水线：创建 build run record，写入 resolved config，执行预检，更新 source-set cache，准备当前 run 工作树，接入 feeds/plugins，并执行构建上下文校验。
- Worktree Manifest 写入 `workspaces/<workspace-id>/runs/<build-id>/<run-id>/worktree-manifest.json`，记录 logical worktree id、storage driver、物理路径或 Docker volume、container path、source-set snapshot、case-sensitive 结果和 applied adopted patch ids。
- `host-path` 与 `linux-path` 从 source-set OpenWrt cache 创建独立可写 Git 工作树；`docker-volume` 记录宿主逻辑 pointer，并通过 resolved `docker.image` 作为一次性 helper container 准备 volume 工作树。
- 同 `workspace_id/build_id` 的 success lock 若存在，Source Manager 会读取 adopted patch ids 并在工作树准备阶段按顺序执行 `git apply --check` 与应用；缺失或失败时阻断在 `worktree.prepare` 并返回退出码 `4`。
- Feeds/plugins 接入会生成 `feeds.conf`、复制 package/unknown plugin 到当前 run 工作树、检查并应用 patch plugin，并写入 `plugin-attach-summary.json`。
- 构建上下文校验覆盖 manifest、工作树存在性与可写性、OpenWrt target/subtarget/profile、Docker mapping/mount scope、plugin attach summary、plugin risk 和 AI worktree access；阻断项返回退出码 `3` 且不进入 D5。
- D4 不生成固件产物，不写 success lock，不写 artifact staging，不写 failure-index，不调用 AI 修复。

### D5：Docker 构建执行

```text
increment_id: D5
goal: Docker 构建执行
upstream_docs: docs/05-implementation-plans/phase-2-build-pipeline.md, docs/03-specs/docker-executor-spec.md, docs/03-specs/config-spec.md, docs/03-specs/run-record-state-spec.md
changed_modules: internal/app, internal/buildconfig, internal/dockerexec
created_artifacts: build.config 阶段、.auto-openwrt/config-fragments 生成器、docker-volume helper 接入、Docker Executor、docker-build.log、docker-env-summary.json、D5 app/source/buildconfig/health/dockerexec 测试
verification: go test ./...；go run ./cmd/doccheck；git diff --check
known_limits: AI 修复仍属于 D7-D8
```

验收结论：

- `build` 在 `health.build_context` 后进入 `build.config`、`docker.build` 和 `build.result`，不再停在 `DOCKER_BUILD_NOT_IMPLEMENTED`。
- `build.config` 为当前 run 工作树生成 target/subtarget/profile、用户 fragments 和 packages 配置片段；缺失 fragment 返回结构化错误。
- `docker-volume` 会通过 resolved Docker image 的 helper container 写入 `feeds.conf`、复制 package/unknown plugin、应用 patch plugin、校验 target/profile，并把构建配置片段复制到 volume 内 `/openwrt/.auto-openwrt/config-fragments/`。
- Docker Executor 只接收 resolved config、manifest、attach summary 和阶段路径，固定执行规格中的 OpenWrt 构建命令序列。
- Docker mount 覆盖当前 run 工作树、下载缓存、构建缓存、artifact staging 和必要只读 source-set cache；测试验证不挂载 project root。
- `docker.platform: auto` 不传递 `--platform`；非 `auto` platform 原样传递给 `docker run`。
- Docker 启动失败映射退出码 `5`，容器内 OpenWrt 构建失败映射退出码 `6`，日志或摘要写入失败映射退出码 `8`。

### D6：产物与失败诊断

```text
increment_id: D6
goal: 产物与失败诊断
upstream_docs: docs/05-implementation-plans/phase-2-build-pipeline.md, docs/03-specs/artifact-spec.md, docs/03-specs/cli-spec.md, docs/03-specs/run-record-state-spec.md
changed_modules: internal/app, internal/artifact, internal/cli
created_artifacts: Artifact Recorder、docker-volume 产物收集 helper、artifact-index.json、failure-index.json、diagnostic-context.json、last-summary.json、success-lock.json、logs final run 路径输出、D6 app/artifact/CLI 测试
verification: go test ./...；go run ./cmd/doccheck；git diff --check
known_limits: adopted patch 生成与 AI 修复历史仍属于 D7-D8
```

验收结论：

- Docker 构建成功后使用 artifact staging 收集固件、构建日志、resolved config、health report、source 版本、Docker 摘要和 worktree manifest，并原子 finalize 到 final artifact 目录。
- `docker-volume` 成功构建后通过 helper container 从 `/openwrt/bin/targets/<target>/<subtarget>/` 收集固件到 artifact staging。
- 成功归档后写入 `workspaces/<workspace-id>/locks/<build-id>/success-lock.json`，最后才写 run `final_status=succeeded`；success lock 写入失败时 run 不为 `succeeded`。
- Docker/OpenWrt 构建失败后写入 diagnostics 目录、`diagnostic-context.json`、`last-summary.json` 和 `failure-index.json`，失败 run 不更新 success lock。
- `logs` 返回 final run 的真实 final status，并透出 run record 中的 artifact、diagnostics、Docker 日志和摘要路径。
- `logs --latest` 仍只基于 final run record 查找，不读取 artifact staging 或未完成 run。

## 完成标准

v1 开发完成必须同时满足：

- D0 到 D8 全部完成并有验证证据。
- 每个 public CLI 行为都有成功和失败路径测试。
- 每个持久化文件都有 schema、路径、写入时机和失败语义测试。
- 每个 Phase 的验收项均可由测试、运行记录或人工验收证据证明。
- 最新一次成功构建能追溯到配置、源码版本、health report、Docker 摘要、产物、success lock 和 adopted patch。

---
status: accepted
owner: architecture
last_updated: 2026-06-06
depends_on:
  - docs/01-product/product-planning.md
---

# ADR-0001：采用单体 CLI + 项目根目录 / 工作区核心模型 + 流水线编排

## 背景

产品面向个人玩家，需要简化安装、部署和构建操作，同时支持源码更新、自定义 feeds、插件构建、失败诊断和 AI 修复。

## 决策

采用单体 CLI 作为用户入口，以 project root 作为外层文件边界，以 workspace 作为配置文件对应的长期状态边界，并以流水线组织构建流程。

## 理由

- 单体 CLI 降低个人用户使用门槛。
- project root 适合维护配置、源码缓存和共享缓存；workspace 适合维护构建记录、日志、产物、补丁和失败现场。
- 流水线能让构建失败定位、AI 修复和重试更清晰。

## 影响

- v1 不把 Web UI 或远程服务作为主入口。
- 构建状态必须归属到 workspace 和 run record。
- 后续远程构建可以作为执行器扩展，而不是替代核心模型。

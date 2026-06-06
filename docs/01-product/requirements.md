---
status: accepted
owner: product
last_updated: 2026-06-06
depends_on:
  - docs/01-product/product-planning.md
---

# 产品需求

## 核心需求

- 用户可以通过一份配置描述 OpenWrt 源码、target、feeds、插件、构建选项、更新策略和 AI 修复策略。
- 用户可以在同一 project root 下维护多份配置文件，每份配置文件拥有独立 workspace 状态。
- 用户可以在 macOS、Linux、WSL 上通过统一 CLI 发起构建。
- 系统支持任意 OpenWrt target、subtarget 和 device profile 的声明和校验。
- 系统可以接入自定义 feeds 和插件源码。
- 系统默认更新到指定分支的最新源码。
- 系统在成功构建后记录 OpenWrt、feeds、插件源码版本。
- 系统在构建前执行健康检查。
- 系统在构建失败后生成诊断上下文。
- 系统可以调用外部 AI CLI 尝试修复源码并重试构建。
- 系统最多执行 5 轮 AI 修复重试。
- 系统在 AI 修复后的构建成功时自动采纳修复差异为可审计补丁。
- 系统成功后输出完整固件和构建记录。

## 用户价值

- 降低 OpenWrt 构建环境准备成本。
- 降低自定义插件和 feeds 接入成本。
- 降低构建失败后的定位和修复成本。
- 让长期维护固件配置的个人用户可以持续更新、构建和回溯。

## 产品验收

- 用户不需要手动进入 OpenWrt 源码目录执行复杂命令。
- 用户可以看到每次构建对应的配置、版本、日志和产物。
- 同一 project root 下不同配置文件可以构建不同固件版本，且不会共享 success lock、adopted patches 或运行工作树。
- 健康检查失败时，用户能看到明确原因和修复建议。
- AI 修复失败后，用户能继续查看失败现场和修复历史。
- AI 修复成功后，用户能追溯 adopted patch、来源 run 和源码版本。

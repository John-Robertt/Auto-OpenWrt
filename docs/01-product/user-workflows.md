---
status: accepted
owner: product
last_updated: 2026-06-07
depends_on:
  - docs/01-product/product-planning.md
---

# 用户工作流

## 首次初始化

用户创建 Auto-OpenWrt project root，获得示例配置和文档入口。

成功结果：

- project root 结构可用。
- 示例配置可编辑。
- 用户知道下一步运行健康检查或构建。

## 构建前检查

用户运行健康检查，或在构建命令中自动触发健康检查。

成功结果：

- Docker、AI CLI、权限、磁盘、网络、配置、project root、workspace 和运行工作树存储状态通过检查。
- 构建命令触发的检查报告被记录到构建记录中；独立健康检查生成独立报告。

失败结果：

- 构建被阻断。
- 用户得到明确原因和修复建议。

## 常规构建

用户选择一个 build 发起完整固件构建。

成功结果：

- 系统创建当前 run record 和运行工作树。
- 系统更新当前配置和 build 对应的 source-set 源码缓存。
- 系统接入 feeds 和插件。
- 系统生成构建配置并执行 Docker 构建。
- 系统归档固件、日志、健康检查报告、解析后配置、Docker 环境摘要和版本记录。

## 构建失败与 AI 修复

构建失败后，系统生成诊断上下文并调用 AI CLI 尝试修复当前 run 工作树。

成功结果：

- 修复前检查点被记录。
- AI 修改当前 run 工作树后重新构建。
- 最多 5 轮内成功则归档产物。
- 系统自动把最终差异归档为 adopted patch，并写入 success lock。

失败结果：

- 系统停止重试。
- 系统保留最后现场、诊断包、检查点和所有修复历史。
- 系统不生成 adopted patch，不更新 success lock。

## 多配置与多 Build 维护

用户在一个 project root 中维护多份配置文件，每份配置文件对应一个独立 workspace；每个 workspace 内可以维护多套 build。

成功结果：

- 不同配置文件可以声明不同 OpenWrt、feeds 和 plugins 版本。
- 每份配置文件有独立 run record、success lock、adopted patches、诊断和产物。
- 每个 build 有独立 OpenWrt target、配置、运行工作树、日志、产物、构建记录和 adopted patches。
- 多配置和多 build 不会共享可能导致混淆的可变状态。

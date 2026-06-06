---
status: accepted
owner: architecture
last_updated: 2026-06-06
depends_on:
  - docs/01-product/product-planning.md
---

# ADR-0002：Docker 作为 OpenWrt 构建执行边界

## 背景

产品需要让用户在 macOS、Linux、WSL 上获得尽量一致的构建体验。OpenWrt 构建对系统依赖、文件系统和工具链有明确要求。

## 决策

CLI 在宿主侧运行，Docker 作为 OpenWrt 构建执行边界。

## 理由

- Docker 能屏蔽大部分宿主依赖差异。
- 宿主 CLI 更容易调用用户本机 AI 工具和读取用户配置。
- OpenWrt 构建环境可以被容器化管理。

## 影响

- 健康检查必须验证 Docker 安装、权限和容器启动能力。
- 当前 run 工作树、缓存和产物目录必须明确映射到 Docker 构建环境，并写入 run record。
- 构建环境一致性依赖 Docker 镜像、源码版本和配置共同保证。
- 运行工作树隔离由 ADR-0004 在本决策基础上约束；本 ADR 不依赖 ADR-0004，避免决策依赖成环。

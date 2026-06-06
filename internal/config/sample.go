package config

const SampleWorkspaceID = "auto-openwrt"

const SampleYAML = `version: 1

workspace:
  id: auto-openwrt
  name: auto-openwrt
  worktree_storage: auto
  linux_worktree_root: ""

openwrt:
  repo: https://github.com/openwrt/openwrt.git
  branch: openwrt-24.10
  update: latest

docker:
  image: ghcr.io/auto-openwrt/build-env:openwrt-24.10
  platform: auto

builds:
  - id: x86-64
    openwrt:
      target: x86
      subtarget: "64"
      profile: generic
    feeds:
      - packages
      - luci
    plugins:
      - openclash
    config:
      fragments: []
      packages: []
      jobs: auto

feeds:
  - name: packages
    repo: https://github.com/openwrt/packages.git
    branch: openwrt-24.10
    path: feeds/packages
    enabled: true
  - name: luci
    repo: https://github.com/openwrt/luci.git
    branch: openwrt-24.10
    path: feeds/luci
    enabled: true

plugins:
  - name: openclash
    type: package
    repo: https://github.com/vernesong/OpenClash.git
    branch: master
    path: luci-app-openclash
    enabled: true
    risk: luci-app

health:
  min_disk_gb: 80

ai_repair:
  enabled: false
  command: ""
  args: []
  timeout: 30m
  max_retries: 5
  adoption: auto

artifacts:
  retention: keep-all
`

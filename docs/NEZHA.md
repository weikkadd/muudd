# 可选哪吒监控

现有容器可同时运行固定版本的官方 Nezha Agent v2.3.5，不创建额外计算服务。未设置 NZ_* 时不启动 Agent。

## 平台 Secrets

仅在目标 compute 服务设置 `NZ_SERVER`、`NZ_CLIENT_SECRET`、`NZ_UUID`、`NZ_TLS`。`NZ_UUID` 使用独立且固定的随机 UUID，不复用代理身份。`NZ_TLS` 必须为 `true`，证书验证不能关闭。真实值不要写入仓库、构建参数或公开日志。

Agent 配置在运行时写入权限 0600 的临时文件。子程序只继承 PATH、HOME、TMPDIR、TZ，不继承 VLESS、WebSocket 或云平台凭证。原始 Agent 输出经过固定分类白名单，不公开原文。

远端命令、终端、文件管理、配置修改、自动/强制升级、NAT 与远端 TCP/ICMP/HTTP 探测均通过 Agent 安全开关禁用。Agent 随主程序启动/停止，不安装系统服务，不设置自动重启；Agent 自身的网络断线重连仍存在。

## 用量与休眠

哪吒每 4 秒上报状态，出站流量可能阻止 InstaCloud 闲置休眠，即使平台 `always_on=false`。这是常驻监控，不代表免费永久在线；Free Plan 额度耗尽后接受平台暂停，不升级、不添加付款方式。本次启用常驻监控已获用户授权。

## 固定来源

- [官方 v2.3.5 release](https://github.com/nezhahq/agent/releases/tag/v2.3.5)
- [官方 checksums.txt](https://github.com/nezhahq/agent/releases/download/v2.3.5/checksums.txt)
- Linux amd64 ZIP SHA-256：`cb0ace70db050a8485d6314535922ae7614ad74c89689fdaab76ecfd5a81ba96`
- 固定 release 为 GitHub immutable release；清单、GitHub asset digest 与实际下载哈希已交叉核对。官方未提供独立签章资产，仍需信任官方 GitHub/CI 供应链。
- 二进制未修改；Apache-2.0 原文保存在 `licenses/nezha/LICENSE`，镜像中亦保留该授权条款。

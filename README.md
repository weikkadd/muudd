# InstaCloud VLESS + WebSocket 节点

为 InstaCloud 的单一 HTTP 入口准备的私人代理模板：**VLESS → WebSocket → 平台 HTTPS/TLS 入口**。仓库文件已准备不等于节点已上线；本仓库没有自动部署工作流，也不创建任何云资源。

## 连接方式

```text
客户端（VLESS + WS + TLS，443）
  → InstaCloud HTTPS 入口（TLS 在平台终止）
  → 容器 HTTP :8080（Go 转发器）
  → 127.0.0.1:10000（Xray）
  → 公网目标 TCP 服务
```

公网只能使用 HTTPS/WSS；不要用明文 WS。平台能看到 TLS 终止后的代理数据，因此必须信任平台内部链路。Xray 仅监听容器回环，不直接暴露。

这是代理节点，不是原生 WireGuard/OpenVPN，也不提供系统 TUN 接口。本模板主动阻断 UDP（包括 QUIC），不承诺游戏、语音或 UDP DNS 兼容；浏览器可回落 TCP，客户端 DNS 应使用受支持的 TCP/DoH 方式。

## 未来部署前需要填写

| 变量 | 用途 |
| --- | --- |
| `VLESS_UUID` | 随机 UUID，必填；是访问凭据，存平台 Secret |
| `WS_PATH` | `/` 加 12–128 位字母、数字、`_`、`-`，必填；建议随机生成 |
| `PORT` | 默认 `8080`，与平台监听端口一致；不能为 `10000` |

在可信设备生成后，手动保存到平台环境变量/Secret，不要把结果写进仓库：

```bash
python3 -c 'import uuid,secrets; print("VLESS_UUID="+str(uuid.uuid4())); print("WS_PATH=/"+secrets.token_urlsafe(24))'
```

`.env.example` 只有占位值，不能直接运行。没有默认 UUID、公开订阅地址、配置下载接口或网页管理面板。错误或缺失配置会拒绝启动。

## 文件说明

- `Dockerfile`：linux/amd64 镜像；固定 Xray `v26.3.27`，校验官方资产 SHA-256，非 root 运行。
- `cmd/node/`：无第三方 Go 依赖的 WS 转发、健康检查、配置校验及子进程管理。
- `scripts/client_link.py`：在本地生成客户端导入链接，不上传任何信息。
- [InstaCloud 操作清单](docs/INSTACLOUD.md)：未来部署步骤、验证和排错。

## 本地检查

```bash
go test ./...
go vet ./...
# Docker 可用时执行；构建不等于创建云服务。
docker build --platform linux/amd64 -t muudd:local .
# 检查变量和 Xray 配置，不监听服务端口。
docker run --rm --env-file .env muudd:local check
# 仅本机开放测试入口，不对公网提供明文 WS。
docker run --rm --env-file .env -p 127.0.0.1:8080:8080 muudd:local
```

不要直接将本地明文测试端口暴露到公网。`GET /healthz` 返回 `ok` 表示 Xray 本地监听可达；这不等于公网 WS 或真实代理出站已经验证。其他普通 HTTP 请求返回 404。

## 安全与维护

仅用于你有权限的网络和符合当地法律、平台条款的用途。**HTTP/WS 能运行不代表平台允许代理用途**，实际部署前应确认 InstaCloud 的使用政策。不要用于开放代理、滥发流量或绕过未经授权的网络。

默认关闭访问日志，阻断 UDP、常见私网/回环/链路本地地址及元数据地址。此规则只是应用层防护，不是网络防火墙，不能完全消除 DNS 重绑定或平台内部地址风险；需要强隔离时应使用平台出站策略或独立 VPS。修改规则前应理解风险。

UUID 和完整 VLESS 链接均视为秘密；泄露后更换 UUID 并重启服务。WS 路径不是认证替代品。镜像不跟随 Xray `latest`，更新版本时同步审核官方发布说明、下载校验和并重新测试。

## 官方参考

- [InstaCloud：部署、GitHub 与 WebSocket 模式](https://docs.instacloud.com/deploy/overview)
- [InstaCloud：仅单一 HTTP 端口、不能暴露原始 TCP](https://docs.instacloud.com/limitations/overview)
- [InstaCloud：运行生命周期](https://docs.instacloud.com/compute/overview)
- [Xray VLESS](https://xtls.github.io/en/config/inbounds/vless.html) · [WebSocket](https://xtls.github.io/en/config/transports/websocket.html) · [官方发行版](https://github.com/XTLS/Xray-core/releases/tag/v26.3.27)

## 本次验证状态

已在隔离工作区使用官方 Xray 二进制完成：`go test -race ./...`、`go vet ./...`、Xray 配置检查、Python 语法与链接输入校验，以及 `scripts/smoke_test.py` 的 WS 101、真实 VLESS→WS→公网 HTTPS、错误 UUID 拒绝、回环目标阻断和子进程异常传播测试。测试仅使用临时凭据，进程结束后清理。

尚未验证：Docker 镜像实际构建、InstaCloud 部署/公网证书/WS 入口、平台休眠与长连接上限、各客户端导入兼容性。当前环境没有 Docker，因此没有声称完成镜像构建或平台上线测试。

复现端到端测试（仅在隔离工作区运行，会临时监听测试端口并访问 example.com）：

```bash
go build -o /tmp/muudd-node ./cmd/node
python3 scripts/smoke_test.py /tmp/muudd-node /path/to/official/xray
```

# InstaCloud 手动部署清单（尚未执行）

本次仅准备 GitHub 文件。没有创建或启动 InstaCloud compute，没有绑定项目或配置仓库自动部署，没有提供真实节点地址。以下步骤留待你明确允许创建云服务后再做；它们可能产生平台用量。

## 1. 部署前

1. 确认平台条款允许私人代理用途，并理解平台 TLS 终止的信任边界。
2. 选择 linux/amd64 服务。本镜像有明确的架构检查；不支持直接改为 arm64。
3. 准备随机 `VLESS_UUID` 与 `WS_PATH`，设为平台 Secret/环境变量。不要放在 GitHub 文件、构建参数或公开日志里。
4. 确认公网 HTTPS 域名和有效证书，容器入口端口为 `8080`；无需自己申请容器内 TLS 证书。
5. 不需要数据库、存储桶、持久卷、额外 TCP 端口或特权容器。

## 2. 未来发布

官方支持连接 GitHub 仓库后随 push 部署；**当前没有设置这种连接**。在希望开始前，不要将本仓库接到自动部署的项目。若以后接入，确认根目录 Dockerfile、WebSocket 模式、端口和环境变量都已配置。

官方明确要求 WebSocket 应用使用 `--websocket`，以启用对应实例与连接并发模型。若控制台无法确认该选项，未来可在选定项目/分支/compute group 后按官方 CLI 执行：

```bash
# 仅未来获得创建/部署授权后运行；本次未运行。
insta deploy . --websocket --port 8080
# 有多个 group 时必须显式选择目标，避免操作其他服务：
# insta deploy . --websocket --port 8080 --group <目标名称>
```

此命令不是纯配置检查，它会构建并部署。不要放入 GitHub Actions。也不要用保活请求、循环 ping 或未经评估的 always-on 设置来避免休眠。

官方部分页面对默认是否保持温暖的表述并不一致，应以创建后实际 compute 配置为准。休眠会带来首次连接等待；重启、发布或平台连接超时会断开长连接。客户端需要重连；本方案没有实测平台最大连接时长。

## 3. 客户端参数

| 项目 | 值 |
| --- | --- |
| 协议 | VLESS |
| 地址 | 实际平台 HTTPS 域名，不带 `https://` |
| 公网端口 | `443` |
| UUID | 与平台 `VLESS_UUID` 一致 |
| 加密 | `none` |
| 传输 | WebSocket / ws |
| WS Path | 与 `WS_PATH` 完全一致 |
| WS Host | 实际平台域名 |
| TLS | 开启 |
| SNI / serverName | 实际平台域名 |
| 跳过证书验证 | 关闭 |
| Flow、REALITY、Early Data | 留空/禁用 |
| UDP | 禁用；本模板在服务端阻断 |

本地使用 `python3 scripts/client_link.py`，按提示输入域名、UUID 和 WS 路径，即可生成 `vless://` 导入链接。UUID 用隐藏输入；输出链接本身包含凭据，不要分享、截图或提交。各客户端支持的链接参数有差异，导入后核对上表。

## 4. 上线后才做的验证

- `https://<域名>/healthz` 应返回 `ok`。根路径 404 是预期结果。
- 使用正确路径做 WS 握手应为 HTTP `101`；普通 GET 到该路径返回 404。
- 用正确 UUID 从客户端访问正常 HTTPS 网站，并确认看到的是节点出站地址。
- 用错误 UUID 验证不能代理；访问私网目标应失败。
- 健康检查通过但连接失败，不要因此关闭 TLS 校验；检查 WS 模式、UUID、路径、Host/SNI 及证书。
- `502/503`：检查容器启动、变量、端口和平台日志。不要将完整配置或节点链接贴进公开日志。

## 5. 停用和恢复

未来停用时，在平台停止对应服务，并确认它的实际状态。仅删除 GitHub 文件不等于停止已有云服务。平台官方暂不提供部署历史回滚；恢复时重新部署已知提交/镜像，保持密钥通过平台管理，不回写旧凭据。

参考：[官方部署说明](https://docs.instacloud.com/deploy/overview)、[计算服务说明](https://docs.instacloud.com/compute/overview)、[限制](https://docs.instacloud.com/limitations/overview)。

# MobileVC Relay Docker Compose 部署

这个目录提供公网中继服务的 Docker Compose 部署模板。默认方式是：

- 自建带 DNS 插件的 Caddy 镜像
- Caddy 通过 DNS-01 自动为 `RELAY_DOMAIN` 申请和续期 HTTPS 证书
- 公网只需要开放 `9443/tcp`
- 手机和本地 MobileVC 节点使用 `wss://relay.example.com:9443`
- relay 服务只在 Docker 内部监听 `9000`，不直接暴露到公网

公网 relay 只负责转发中继连接。它暴露 `/healthz`、`/version`、`/relay/agent`、`/relay/client`；MobileVC 的会话 websocket 和文件下载都走 E2EE 加密隧道，不应该在 relay 上明文暴露 `/ws` 或 `/download`。

## 1. 准备域名

把你的域名 A 记录解析到部署服务器公网 IP，例如：

```text
relay.example.com -> 服务器公网 IP
```

服务器安全组/防火墙只需要放行：

```text
9443/tcp
```

DNS-01 不需要开放 `80/tcp` 或 `443/tcp`。证书验证会通过 DNS API 创建 `_acme-challenge` TXT 记录完成。

## 2. 选择 DNS 服务商

默认镜像支持 Cloudflare 和阿里云 AliDNS。Cloudflare 配置：

```dotenv
CADDY_DOCKERFILE=Caddy.DNS.Dockerfile
CADDYFILE_PATH=./Caddyfile.cloudflare
CF_API_TOKEN=replace-with-cloudflare-token
```

Cloudflare Token 建议只授权目标 Zone：

```text
Zone:Read
DNS:Edit
```

阿里云 AliDNS：

```dotenv
CADDY_DOCKERFILE=Caddy.DNS.Dockerfile
CADDYFILE_PATH=./Caddyfile.alidns
ALIYUN_ACCESS_KEY_ID=replace-with-access-key-id
ALIYUN_ACCESS_KEY_SECRET=replace-with-access-key-secret
```

腾讯云 DNSPod 的 Caddy 插件比较旧，不能和当前 Caddy 2.11 一起构建。模板提供单独的 legacy 镜像，确认需要 DNSPod 时这样配置：

```dotenv
CADDY_DOCKERFILE=Caddy.DNSPod.Dockerfile
CADDYFILE_PATH=./Caddyfile.dnspod
DNSPOD_TOKEN=APP_ID,APP_TOKEN
```

不要把真实 DNS token、pairing 链接或 secret 提交到仓库。

## 3. 配置

```bash
cd deploy/relay
cp .env.example .env
```

编辑 `.env`：

```dotenv
RELAY_PUBLIC_URL=wss://relay.example.com:9443
RELAY_DOMAIN=relay.example.com
CADDY_HTTPS_PORT=9443
CADDY_DOCKERFILE=Caddy.DNS.Dockerfile
CADDYFILE_PATH=./Caddyfile.cloudflare
CF_API_TOKEN=replace-with-cloudflare-token
```

把 `relay.example.com` 换成你自己的中继域名，并按你的 DNS 服务商填写 token。

## 4. 启动

```bash
docker compose up -d --build
```

查看日志：

```bash
docker compose logs -f relay
docker compose logs -f caddy
```

第一次启动时，`caddy` 日志里应该能看到 DNS challenge、申请证书和启用 TLS 的记录。如果 DNS token 权限不够、域名不在这个账号下，或者 DNS 传播超时，Caddy 会直接报错。

停止：

```bash
docker compose down
```

## 5. 验证

```bash
curl -fsS https://relay.example.com:9443/healthz
curl -fsS https://relay.example.com:9443/version
```

`/healthz` 应该返回：

```text
ok
```

## 6. 本地电脑连接中继

公网 relay 启动后，在本地电脑启动 MobileVC：

```bash
mobilevc public --relay wss://relay.example.com:9443
```

`mobilevc` 是 npm 安装的 Node.js launcher：

```bash
npm install -g @justprove/mobilevc
```

这条命令在你的本地电脑上运行，不是在 VPS 上运行。它会启动本地 MobileVC 后端，连接公网 relay，并打印手机扫码用的 relay 配对二维码。VPS 上只需要运行上面的 relay Docker Compose 服务。

这个模式会同时保留局域网直连和 relay 连接。

如果你只想让本地后端监听 `127.0.0.1`，只通过 relay 给手机连：

```bash
mobilevc public --relay wss://relay.example.com:9443 --network-exposure-mode relay-only
```

启动后终端会打印 relay 二维码。手机扫码或导入 `mobilevc://relay/v1?...` 链接即可配对。

## 7. 源码启动本地后端

不用 npm launcher 时，可以从源码启动本地后端：

```bash
AUTH_TOKEN="replace-with-local-token" go run ./cmd/server \
  --relay-mode=true \
  --relay-url wss://relay.example.com:9443 \
  --relay-pairing-event-path /tmp/mobilevc-relay-pairing.json \
  --network-exposure-mode lan
```

`--relay-pairing-event-path` 会写出一次性配对事件。launcher 会读取它、生成二维码，然后删除这个文件。不要公开这个文件内容或 pairing 链接。

## 8. 生产安全默认值

`.env.example` 默认开启：

```dotenv
RELAY_REQUIRE_E2EE=true
RELAY_PLAINTEXT_TEST_MODE=false
```

公网部署不要打开 plaintext test mode。`RELAY_TRUSTED_PROXY_CIDRS` 只应该配置为 Docker/Caddy 或你真实反代所在的私有网段，不要写 `0.0.0.0/0`。

## 9. 常见错误

`caddy` 证书申请失败

确认 DNS token 对当前域名有权限、域名已经使用对应 DNS 服务商解析、`CADDY_DOCKERFILE`、`CADDYFILE_PATH` 和 token 类型匹配。Cloudflare 需要 `Zone:Read` 和 `DNS:Edit`；DNSPod token 格式是 `APP_ID,APP_TOKEN`，并且要使用 `Caddy.DNSPod.Dockerfile`。

`pairing_rejected`

配对链接过期、已被使用，或和当前 relay session 不匹配。重新运行 `mobilevc public --relay ...` 生成新的二维码。

`device_unknown`

手机保存的是旧的设备凭证，或者本地电脑的 node identity 已轮换。需要重新配对。

`e2ee_handshake_failed` / `e2ee_decrypt_failed`

手机 APK、本地后端、relay 不是同一套协议版本，或导入了旧链接。重启/重打包相关组件后重新生成二维码。

`payload_too_large`

单个中继 payload 超过 `RELAY_MAX_PAYLOAD_BYTES`。默认上限是 32 MiB；文件下载应该走 E2EE chunk/backpressure 流，不要一次性塞进一个 relay frame。

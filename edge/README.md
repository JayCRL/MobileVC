# MobileVC Edge

给已经跑着 DSH 的用户一个**自部署远程入口**：Caddy 反代 + 登录保护 + 可选 Cloudflare Tunnel。

它不包含 DSH 本身，也不包含 Agent runtime：

```text
Phone / PWA
  │ HTTPS
  ▼
Cloudflare Tunnel (optional)
  │
  ▼
Caddy :8088 / :443
  │ basic_auth
  │ SSE: flush_interval -1
  │ upload: 25MB
  ▼
127.0.0.1:8898
  │
  ▼
DSH + mobilevc plugin
```

## 前置

- Linux（Docker host network 模式）
- Docker + Docker Compose
- 已经装好 DSH，能访问 `http://127.0.0.1:8898/mobilevc/api/health`
- 已经装 MobileVC 插件：

```bash
dsh plugin --profile web add "github:JayCRL/MobileVC"
systemctl restart dsh-web
```

## 方式 A：没有公网 IP（推荐）

用 Cloudflare Tunnel，Caddy 只在本机监听 `:8088`。

### 1. 准备配置

```bash
cd edge
cp .env.example .env
```

生成 basic auth 密码哈希：

```bash
./hash-password.sh
# 或者：
docker run --rm caddy:2.8-alpine caddy hash-password --plaintext 'your-password'
```

编辑 `.env`：

```bash
DSH_UPSTREAM=127.0.0.1:8898
SITE_ADDRESS=http://:8088
BASIC_AUTH_USER=mobilevc
BASIC_AUTH_HASH='$2a$14$...'     # 保留单引号
MAX_UPLOAD=25MB
CF_TUNNEL_TOKEN=...               # 下一步从 Cloudflare 复制
```

### 2. 建 Cloudflare Tunnel

Cloudflare Zero Trust → Networks → Tunnels → Create a tunnel → Cloudflared：

- Public hostname：`dsh.example.com`
- Service：`http://localhost:8088`

把 tunnel token 填进 `.env` 的 `CF_TUNNEL_TOKEN`。

建议再加一个 **Cloudflare Access** 策略（邮箱 / OTP / SSO），不要让域名裸奔。

### 3. 启动

```bash
docker compose --profile tunnel up -d
docker compose logs -f caddy
```

打开：

```text
https://dsh.example.com/mobilevc/
```

Cloudflare 负责 TLS，Caddy 不需要 80/443。

> 如果你的 DSH 在另一台机器，而不是 Docker 宿主机，改 `DSH_UPSTREAM`；并确保 Caddy 容器能访问到它。

## 方式 B：有公网服务器 / 域名

不用 Cloudflare Tunnel，让 Caddy 直接做 HTTPS。

1. DNS 把 `dsh.example.com` 指到这台机器
2. 防火墙放行 80/443
3. `.env`：

```bash
SITE_ADDRESS=https://dsh.example.com
DSH_UPSTREAM=127.0.0.1:8898
BASIC_AUTH_USER=mobilevc
BASIC_AUTH_HASH='$2a$14$...'
```

4. 启动：

```bash
docker compose up -d caddy
```

Caddy 会自动申请证书，数据存在 `caddy_data` volume。

## 认证

`/mobilevc/` 是 DSH Host plugin 注册的路径，不走 DSH `/api` 的 browser-trust fence，所以**必须**在 edge 上认证。

默认使用 Caddy `basic_auth`：

- 用户名：`BASIC_AUTH_USER`
- 密码哈希：`BASIC_AUTH_HASH`（bcrypt）
- 不要填明文密码
- `.env` 里的哈希用单引号包住，避免 `$` 被展开

生产环境建议再套一层 Cloudflare Access 或 OAuth；Basic Auth 作为兜底。

## 关键配置

| 配置 | 作用 |
|---|---|
| `flush_interval -1` | SSE follow 不缓冲；否则思考流会像卡住 |
| `MAX_UPLOAD=25MB` | 图片/文件附件；UI 自身限制 12MB/文件、20MB/总 |
| `network_mode: host` | DSH 只监听 127.0.0.1，容器必须和宿主机共享网络 |
| `SITE_ADDRESS=http://:8088` | Tunnel 模式，Cloudflare 终止 TLS |
| `SITE_ADDRESS=https://...` | 直连模式，Caddy 终止 TLS |
| `CF_TUNNEL_TOKEN` | 只在 `--profile tunnel` 使用 |
| `/dsh-startup/*` | 可选启动动画插件 `dsh-startup-screen` 的运行时 |

## DSH 的 trusted host

如果 DSH Web 启动时用了 `--trusted-host`，把公开域名加进去，例如：

```bash
dsh web --host 127.0.0.1 --port 8898 --trusted-host dsh.example.com
```

## 验证

```bash
curl -u mobilevc:your-password \
  https://dsh.example.com/mobilevc/api/health
```

应该返回：

```json
{"ok":true,"plugin":"mobilevc","interactive":true}
```

SSE 测试：

```bash
curl -N -u mobilevc:your-password \
  "https://dsh.example.com/mobilevc/api/follow?session=<session-id>"
```

应该先收到 `snapshot`，不是等到超时才一次性返回。

## 常见问题

**Caddy 启动失败：basic_auth hash invalid**

- 哈希必须是 bcrypt，不是明文
- `.env` 里保留单引号
- 重新生成：

```bash
docker run --rm caddy:2.8-alpine caddy hash-password --plaintext 'your-password'
```

**手机能打开首页，但消息不更新**

- 检查 `flush_interval -1`
- 检查中间是否还有别的代理缓冲 SSE
- 看 `docker compose logs caddy`

**413 上传失败**

- 调大 `MAX_UPLOAD`
- 如果前面还有 Cloudflare/nginx，也要调它们的 body limit

**Caddy 无法访问 127.0.0.1:8898**

- 确认用了 `network_mode: host`
- 确认 DSH 监听 `127.0.0.1:8898`
- 在容器内测试：`docker compose exec caddy wget -qO- http://127.0.0.1:8898/mobilevc/api/health`

**Cloudflare Tunnel 启动失败**

- 确认 `CF_TUNNEL_TOKEN` 是 tunnel token，不是 API token
- 确认 public hostname 的 service 是 `http://localhost:8088`
- 看 `docker compose --profile tunnel logs cloudflared`

## 安全

- 不要把 8898 直接暴露公网
- 不要省略认证
- `.env` 已被 `.gitignore` 忽略，不要提交
- `CF_TUNNEL_TOKEN` 等同于隧道凭据
- 所有登录用户共享同一个 DSH 身份，没有多租户隔离
- 需要给不同用户隔离，得另做 Relay / 多实例

## 升级 / 停止

```bash
docker compose pull
docker compose up -d

# 停止
docker compose --profile tunnel down
```

## 限制

- 依赖 Linux Docker 的 host network；macOS Docker Desktop 不支持
- 这不是完整的 dsh-remote，不做 P2P / E2EE / 原生 App / 推送
- 它只解决“没有公网 IP 时怎么安全地暴露 `/mobilevc/`”

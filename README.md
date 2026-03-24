# MobileVC

MobileVC 是一个面向手机浏览器的 Claude Code 移动控制台。

它的目标很直接：**让你离开电脑后，仍然能在手机上继续控制本机的 Claude Code 会话。**

你可以用它来：
- 查看 Claude 当前是否正在执行
- 继续发送指令
- 查看会话、文件、diff、日志和状态
- 恢复历史会话
- 通过 TTS 接口生成语音

> 建议只在本机或可信局域网内使用，不要直接暴露到公网。

---

## 你最应该先看的：3 分钟快速启动

### 1. 准备环境

需要：
- Go
- Claude Code 可在本机正常运行
- 一个你自己设置的 `AUTH_TOKEN`

例如：

```bash
export AUTH_TOKEN=test
```

### 2. 启动后端

在仓库根目录运行：

```bash
AUTH_TOKEN=test go run ./cmd/server
```

默认端口：`8001`

启动成功后，本机健康检查：

```bash
curl http://127.0.0.1:8001/healthz
```

预期返回：

```text
ok
```

### 3. 在浏览器打开

电脑本机访问：

```text
http://127.0.0.1:8001/
```

如果你想在手机上打开，把 `127.0.0.1` 换成电脑的局域网 IP，例如：

```text
http://192.168.1.20:8001/
```

### 4. 输入 token 登录

登录页输入你启动服务时使用的 `AUTH_TOKEN`，例如：

```text
test
```

### 5. 开始使用

进入工作台后，你可以直接输入：

```text
claude
```

这样就能新建一个 Claude Code 会话。

如果已经有会话，也可以直接：
- 打开会话面板
- 恢复历史会话
- 查看当前状态
- 继续向会话发送内容

---

## 最小可用启动方式

如果你现在只想尽快跑起来，下面这组命令就够了：

```bash
cd /path/to/MobileVC
AUTH_TOKEN=test go run ./cmd/server
```

然后在浏览器打开：

```text
http://127.0.0.1:8001/
```

---

## 常用启动方式

### 方式一：仅启动 Web 工作台

适合只用移动端控制 Claude Code，不启用 TTS。

```bash
AUTH_TOKEN=test go run ./cmd/server
```

### 方式二：启动 Web 工作台 + TTS

如果你想启用语音合成，需要先启动 Python sidecar，再启动 Go 服务。

---

## TTS 快速启动

当前 TTS 链路是：

```text
Client -> MobileVC Go server -> Python ChatTTS sidecar
```

### 1. 安装 sidecar 依赖

```bash
cd sidecar/chattts
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

`requirements.txt` 已包含：
- `Flask`
- `numpy`
- `ChatTTS`

### 2. 启动 sidecar

回到仓库根目录后运行：

```bash
PYTHON_BIN="$PWD/sidecar/chattts/.venv/bin/python" \
CHATTTS_MODEL_DIR="$HOME/.cache/mobilevc/chattts" \
bash sidecar/chattts/run.sh
```

默认：
- sidecar 地址：`http://127.0.0.1:9966`
- 模型缓存目录：`~/.cache/mobilevc/chattts`

### 3. 启动 Go 服务并启用 TTS

```bash
AUTH_TOKEN=test \
TTS_ENABLED=true \
TTS_PROVIDER=chattts-http \
TTS_PYTHON_SERVICE_URL=http://127.0.0.1:9966 \
go run ./cmd/server
```

### 4. 验证 TTS

检查 sidecar：

```bash
curl http://127.0.0.1:9966/healthz
```

检查 Go API：

```bash
curl 'http://127.0.0.1:8001/api/tts/healthz?token=test'
```

生成音频：

```bash
curl -X POST 'http://127.0.0.1:8001/api/tts/synthesize?token=test' \
  -H 'Content-Type: application/json' \
  -d '{"text":"你好，欢迎使用 MobileVC。","format":"wav"}' \
  --output /tmp/mobilevc_tts.wav
```

---

## 关于 ChatTTS 模型下载

这个项目**没有单独的 Go 侧模型下载接口**。

这里的“下载模型”，实际是指：
- Python sidecar 初始化 `ChatTTS`
- `ChatTTS` 自己准备模型文件
- 模型缓存放到 `CHATTTS_MODEL_DIR` 指定目录

默认缓存目录：

```text
~/.cache/mobilevc/chattts
```

这意味着：
- 模型不会被提交进仓库
- Go 服务本身不管理模型文件
- TTS 是否能真实工作，取决于 sidecar 是否成功加载模型

---

## sidecar 运行模式

通过 `SIDECAR_MODE` 控制行为：

- `auto`：优先尝试真实 ChatTTS，失败时自动回退到 mock
- `mock`：始终返回本地生成的测试 wav
- `chattts`：强制使用真实 ChatTTS，加载失败就返回 `503`

例如：

```bash
SIDECAR_MODE=mock bash sidecar/chattts/run.sh
```

```bash
SIDECAR_MODE=chattts bash sidecar/chattts/run.sh
```

---

## 常用接口

### 健康检查

```bash
curl http://127.0.0.1:8001/healthz
```

### TTS 健康检查

```bash
curl 'http://127.0.0.1:8001/api/tts/healthz?token=test'
```

### TTS 合成

```bash
curl -X POST 'http://127.0.0.1:8001/api/tts/synthesize?token=test' \
  -H 'Content-Type: application/json' \
  -d '{"text":"你好","format":"wav"}' \
  --output reply.wav
```

### 下载本机文件

```bash
curl 'http://127.0.0.1:8001/download?token=test&path=/tmp/reply.wav' --output reply.wav
```

---

## 当前主要能力

MobileVC 当前支持：
- 手机浏览器登录与连接
- Claude Code 会话启动与恢复
- 文件浏览
- diff 审阅
- 运行日志查看
- 状态详情展示
- Skill / Memory 面板
- TTS REST 接口

---

## 常见问题

### 1. 打不开页面

先检查服务是否已启动：

```bash
curl http://127.0.0.1:8001/healthz
```

### 2. 手机上打不开

通常是这几个问题：
- 你访问的不是电脑的局域网 IP
- 端口没有放通
- 服务只在本机运行但你填错地址

### 3. 登录失败

确认输入的 token 和启动服务时的 `AUTH_TOKEN` 完全一致。

### 4. TTS healthz 是 ok，但没有真实语音

说明你可能跑在 `auto` 模式，sidecar 回退到了 `mock`。

可以检查：

```bash
curl http://127.0.0.1:9966/healthz
```

看返回的 `backend` 是 `chattts` 还是 `mock`。

### 5. `SIDECAR_MODE=chattts` 返回 503

这通常表示：
- `ChatTTS` 依赖没装好
- 当前网络无法完成模型准备
- 模型没有成功加载完成

---

## 项目结构

```text
cmd/server/           Go 服务入口
internal/ws/          WebSocket 与会话控制
internal/store/       会话存储
internal/tts/         Go 侧 TTS 服务层
sidecar/chattts/      Python ChatTTS sidecar
web/                  当前 Web 前端
mobile_vc/            Flutter 客户端工程
```

---

## 一句话总结

如果你只想马上用起来，请记住下面两步：

```bash
AUTH_TOKEN=test go run ./cmd/server
```

然后打开：

```text
http://127.0.0.1:8001/
```

登录后输入：

```text
claude
```

就可以开始用了。

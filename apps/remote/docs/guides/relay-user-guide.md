# MobileVC Relay 用户操作指南

## 安装

```bash
npm install -g @justprove/mobilevc
```

## 首次启动

```bash
mobilevc public --relay wss://relay.mobilevc.top:9443
```

终端打印两个 QR：
- **上方**：局域网直连（同一 WiFi 下更快）
- **下方**：Relay 中继连接（任何网络都能用）

手机扫码配对即可。

## 二维码过期

配对 QR **5 分钟**内有效。过期后运行：

```bash
mobilevc public --relay wss://relay.mobilevc.top:9443
```

后端不会重启，只刷新配对 QR。

## 日常使用

后续启动只需：

```bash
mobilevc start
```

已配对的手机自动重连，无需再扫码。

## 仅用 Relay（不开放局域网）

```bash
mobilevc public --relay wss://relay.mobilevc.top:9443 --network-exposure-mode relay-only
```

## 常用命令

| 命令 | 用途 |
|------|------|
| `mobilevc start` | 启动后端 |
| `mobilevc stop` | 停止后端 |
| `mobilevc restart` | 重启后端 |
| `mobilevc status` | 查看运行状态 |
| `mobilevc logs` | 查看日志 |
| `mobilevc config` | 修改端口/Token |

## 遇到问题

- **二维码过期** → 运行 `mobilevc public --relay wss://relay.mobilevc.top:9443` 刷新
- **设备未绑定** → 重新扫码配对
- **连不上** → 先 `mobilevc status` 确认后端存活，再 `mobilevc logs` 查日志

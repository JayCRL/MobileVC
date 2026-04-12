# 📦 npm 发布总结 v0.1.12

## ✅ 已发布

### 主包
- **@justprove/mobilevc@0.1.12** ✅

### 平台二进制包
- **@justprove/mobilevc-server-darwin-arm64@0.1.12** ✅
- **@justprove/mobilevc-server-darwin-x64@0.1.12** ✅
- **@justprove/mobilevc-server-linux-arm64@0.1.12** ✅
- **@justprove/mobilevc-server-linux-x64@0.1.12** ✅
- **@justprove/mobilevc-server-win32-x64@0.1.12** ✅

## 🔧 本次更新内容

### 修复 Flutter Web 嵌入路径问题
- ✅ 将 Flutter Web 构建产物复制到 `cmd/server/web/`（Go 实际嵌入的位置）
- ✅ 重新构建所有平台二进制，包含完整的 Flutter Web
- ✅ 二进制大小从 ~17MB 增加到 ~36MB（包含 Flutter Web 运行时）

### 用户体验改进
- ✅ 用户安装后直接启动，访问 Web 端即可看到 Flutter Web 版本
- ✅ 无需手动构建 Flutter Web
- ✅ 无需额外配置

## 📦 安装方式

```bash
# 全局安装
npm install -g @justprove/mobilevc

# 启动
mobilevc

# 访问 http://localhost:8001
# 现在会看到完整的 Flutter Web 界面！
```

## 🔍 验证方式

### 检查版本
```bash
npm view @justprove/mobilevc version
# 应该显示 0.1.12
```

### 检查二进制大小
安装后检查二进制大小：
```bash
# macOS
ls -lh ~/.npm/_npx/*/node_modules/@justprove/mobilevc-server-darwin-arm64/bin/mobilevc-server

# 应该是 ~36MB（包含 Flutter Web）
```

### 访问测试
```bash
mobilevc
# 访问 http://localhost:8001
# 应该看到 Flutter Web 界面，而不是简单的 HTML 页面
```

## 📊 版本对比

| 版本 | 二进制大小 | Web 端 | 说明 |
|------|-----------|--------|------|
| 0.1.11 | ~17MB | 旧 HTML | 缺少 Flutter Web 嵌入 |
| 0.1.12 | ~36MB | Flutter Web ✅ | 包含完整 Flutter Web |

## 🔗 npm 链接

- **主包**: https://www.npmjs.com/package/@justprove/mobilevc
- **darwin-arm64**: https://www.npmjs.com/package/@justprove/mobilevc-server-darwin-arm64
- **darwin-x64**: https://www.npmjs.com/package/@justprove/mobilevc-server-darwin-x64
- **linux-arm64**: https://www.npmjs.com/package/@justprove/mobilevc-server-linux-arm64
- **linux-x64**: https://www.npmjs.com/package/@justprove/mobilevc-server-linux-x64
- **win32-x64**: https://www.npmjs.com/package/@justprove/mobilevc-server-win32-x64

## 📝 更新日志

### v0.1.12 (2026-04-09)
- 🔧 修复 Flutter Web 嵌入路径问题
- ✅ 将 Flutter Web 构建产物包含在二进制中
- ✅ 用户安装后直接可用，无需额外构建

### v0.1.11 (2026-04-09)
- 🎉 iOS APNs 推送通知支持
- 🌐 Flutter Web 支持（但嵌入路径错误）
- 🔧 修复会话衔接和重连

## ⚠️ 重要提示

如果用户已经安装了 0.1.11，需要重新安装：

```bash
# 卸载旧版本
npm uninstall -g @justprove/mobilevc

# 安装新版本
npm install -g @justprove/mobilevc

# 验证版本
mobilevc --version  # 应该显示 0.1.12
```

## 🎉 完成

所有包已成功发布到 npm！用户现在可以通过 `npm install -g @justprove/mobilevc` 安装并直接使用 Flutter Web 版本。

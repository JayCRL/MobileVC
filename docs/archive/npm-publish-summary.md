# 📦 MobileVC 发布总结

> Archive note: this document is kept for historical context. Prefer current docs from [docs/README.md](../README.md).



## ✅ 已发布的包

### 主包
- **@justprove/mobilevc@0.1.11** ✅
  - npm: https://www.npmjs.com/package/@justprove/mobilevc
  - 包含 launcher 和文档

### 平台二进制包
- **@justprove/mobilevc-server-darwin-arm64@0.1.11** ✅
- **@justprove/mobilevc-server-darwin-x64@0.1.11** ✅
- **@justprove/mobilevc-server-linux-arm64@0.1.11** ✅
- **@justprove/mobilevc-server-linux-x64@0.1.11** ✅
- **@justprove/mobilevc-server-win32-x64@0.1.11** ✅

## 📦 安装方式

```bash
# 全局安装
npm install -g @justprove/mobilevc

# 使用
mobilevc
```

npm 会根据当前平台自动安装对应的二进制包。

## 🔄 后续发布流程

使用自动化脚本：

```bash
# 一键发布所有包
./scripts/publish-all.sh
```

或手动发布：

```bash
# 1. 更新版本
npm version patch --no-git-tag-version

# 2. 构建二进制
npm run build:binaries

# 3. 更新所有包版本
for dir in packages/mobilevc-server-*; do
  cd "$dir"
  npm version <version> --no-git-tag-version --allow-same-version
  cd ../..
done

# 4. 发布二进制包
for dir in packages/mobilevc-server-*; do
  (cd "$dir" && npm publish)
done

# 5. 更新主包依赖版本
# 编辑 package.json 中的 optionalDependencies

# 6. 发布主包
npm publish
```

## 📝 版本历史

### v0.1.11 (2026-04-09)
- ✅ iOS APNs 推送通知支持
- ✅ Flutter Web 支持
- ✅ 修复会话衔接和重连
- ✅ 更新 README 和文档

### v0.1.10
- 初始 npm 发布版本

## 🔗 相关链接

- **GitHub**: https://github.com/JayCRL/MobileVC
- **npm 主包**: https://www.npmjs.com/package/@justprove/mobilevc
- **官网**: https://www.mobilevc.top

## ⚠️ 注意事项

1. **版本同步**：所有包（主包 + 5 个二进制包）必须保持版本号一致
2. **发布顺序**：先发布二进制包，再更新主包依赖，最后发布主包
3. **二进制构建**：确保在发布前运行 `npm run build:binaries`
4. **测试**：发布后测试安装 `npm install -g @justprove/mobilevc`

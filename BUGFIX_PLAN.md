# 会话管理问题修复方案

## 问题1：会话恢复时好时坏，有时空白

### 根因
`loadSessionRecord` 在 Codex 会话加载失败时，回退逻辑不完善：
- 如果本地缓存的 `existing` 记录是空的，直接返回空记录
- 没有区分"会话不存在"和"临时加载失败"

### 修复方案
在 `internal/ws/handler.go:3287-3314` 增强错误处理：

```go
func loadSessionRecord(ctx context.Context, sessionStore store.Store, sessionID string) (store.SessionRecord, error) {
    if sessionStore == nil {
        return store.SessionRecord{}, fmt.Errorf("session store unavailable")
    }
    
    // 非 Codex 会话，直接从本地加载
    if !codexsync.IsMirrorSessionID(sessionID) {
        return sessionStore.GetSession(ctx, sessionID)
    }
    
    // Codex 会话：先尝试从本地缓存加载
    existing, existingErr := sessionStore.GetSession(ctx, sessionID)
    
    // 尝试从 Codex 原生数据库同步最新状态
    thread, nativeErr := codexsync.FindNativeThread(ctx, sessionID)
    
    // 如果 Codex 同步成功，更新本地缓存
    if nativeErr == nil {
        record := codexsync.MirrorRecord(thread)
        // 保留本地的 SessionContext 和 Catalog Meta
        if existingErr == nil {
            record.Projection.SessionContext = existing.Projection.SessionContext
            record.Projection.SkillCatalogMeta = existing.Projection.SkillCatalogMeta
            record.Projection.MemoryCatalogMeta = existing.Projection.MemoryCatalogMeta
        }
        if _, upsertErr := sessionStore.UpsertSession(ctx, record); upsertErr != nil {
            logx.Warn("ws", "upsert codex mirror session failed: sessionID=%s err=%v", sessionID, upsertErr)
        }
        return sessionStore.GetSession(ctx, sessionID)
    }
    
    // Codex 同步失败，判断是否可以使用本地缓存
    if existingErr == nil && existing.Summary.ID != "" {
        // 本地缓存有效，使用缓存（可能是离线或临时故障）
        logx.Warn("ws", "codex sync failed but using cached record: sessionID=%s nativeErr=%v", sessionID, nativeErr)
        return existing, nil
    }
    
    // 本地缓存也无效，返回错误
    return store.SessionRecord{}, fmt.Errorf("codex session not found and no valid cache: %w", nativeErr)
}
```

## 问题2：Flutter 连接后总有空的桌面版会话

### 根因
`mergeSessionSummaries` 在每次连接时都会主动同步 Codex 会话并写入本地存储

### 修复方案A：延迟同步（推荐）
只在用户主动刷新会话列表时才同步 Codex：

```go
// 在 ServeHTTP 初始化时，不自动合并 Codex 会话
if h.SessionStore != nil {
    items, err := h.SessionStore.ListSessions(ctx)
    if err != nil {
        logx.Warn("ws", "initial session list restore failed: ...")
    } else {
        // 只返回本地会话，不合并 Codex
        emit(protocol.NewSessionListResultEvent(selectedSessionID, toProtocolSummaries(items)))
    }
}

// 在 session_list action 中才合并
case "session_list":
    // ... 现有逻辑保持不变，这里才调用 mergeSessionSummaries
```

### 修复方案B：只读合并（备选）
合并时不写入本地存储，只在内存中展示：

```go
func mergeSessionSummaries(...) ([]store.SessionSummary, error) {
    // ... 前面逻辑不变
    
    for _, thread := range nativeThreads {
        record := codexsync.MirrorRecord(thread)
        if _, ok := seen[record.Summary.ID]; ok {
            continue
        }
        
        // 移除自动 UpsertSession，只在用户真正加载时才写入
        // if sessionStore != nil {
        //     sessionStore.UpsertSession(ctx, record)
        // }
        
        merged = append(merged, record.Summary)
        seen[record.Summary.ID] = struct{}{}
    }
    // ...
}
```

## 推荐实施顺序

1. **先修复问题1**（会话空白）- 影响用户体验
2. **再修复问题2**（空会话）- 优化性能和清洁度

## 测试验证

### 问题1验证
```bash
# 1. 停止 Codex 进程，模拟数据库不可用
pkill -9 codex

# 2. Flutter 加载之前打开过的 Codex 会话
# 预期：应该显示缓存内容，而不是空白

# 3. 重启 Codex，再次加载
# 预期：应该同步最新内容
```

### 问题2验证
```bash
# 1. 清空本地会话存储
rm -rf ~/.mobilevc/sessions/*.json

# 2. Flutter 连接后端
# 预期：不应该自动创建 Codex 镜像会话

# 3. 手动刷新会话列表
# 预期：这时才应该看到 Codex 会话
```

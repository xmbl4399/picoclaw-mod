# 角色记忆隔离修复

**日期**: 2026-07-20
**状态**: ✅ 已部署运行（PID 22683）
**涉及文件**:
- `pkg/health/server.go` — 新增 `/character/clear` 端点
- `/tmp/picoclaw_webui.html` — WebUI 三处调用加 `&id=` 参数
- WebUI 修改版存档: `go_src/pkg/health/webui/picoclaw_webui_mod.html`

---

## 问题

角色记忆隔离完全失效。**根因**: health server 未注册 `/character/clear` HTTP 端点。

WebUI 的 `switchChar()`/`resetChar()`/`newSession()` 均调用
```
POST http://host:18790/character/clear?name=Kirino
```

请求直接落到 `serveWebUI` handler → 返回 HTML 页面 → **后端从未感知到角色切换**。

### 影响

回调链完全断裂：
```
WebUI /character/clear
  → ❌ health server 没有这个路由，落入 serveWebUI
  → SetActiveCharacter 从未被调用
  → characterChangeCallback 从未触发
  → reg.SetCharacterIDForDefault 从未执行
  → ContextBuilder.WithCharacterID 从未执行
  → MemoryStore.SetCharacterID 从未执行
```

结果：
- `MemoryStore.characterID` 始终为 `""`（空字符串）
- 所有角色的长期记忆（MEMORY.md）全部写在同一文件 `memory/MEMORY.md`
- `memory/characters/` 目录完全不存在

---

## 修复方案

### 后端（`pkg/health/server.go`）

在 `NewServer()` 和 `RegisterOnMux()` 中加入路由：

```go
mux.HandleFunc("/character/clear", s.characterClearHandler)
```

新增 Handler：

```go
func (s *Server) characterClearHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    id := r.URL.Query().Get("id")
    if id == "" {
        id = r.URL.Query().Get("name") // fallback
    }
    if id == "" {
        http.Error(w, "missing name or id parameter", http.StatusBadRequest)
        return
    }
    s.SetActiveCharacter(id)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "ok", "character_id": id})
}
```

### WebUI（`/tmp/picoclaw_webui.html`）

三处调用加入 `&id=` 参数：

| 位置 | 原 URL | 修复后 |
|------|--------|--------|
| `switchChar(id)` | `?name=` + name | `?name=` + name + `&id=` + id |
| `resetChar()` | `?name=PicoClaw` | `?name=PicoClaw&id=picoclaw` |
| `newSession()` | `?name=` + name | `?name=` + name + `&id=` + activeCharId |

---

## 回调链（修复后完整路径）

```
WebUI POST /character/clear?name=Kirino&id=kirino
  → health.characterClearHandler
  → s.SetActiveCharacter("kirino")
    → activeCharacter.Store("kirino")
    → characterChangeCallback("kirino") [gateway.go]
      → reg.SetCharacterIDForDefault("kirino") [registry.go]
        → inst.SetCharacterID("kirino") [instance.go]
          → cb.WithCharacterID("kirino") [context.go]
            → memory.SetCharacterID("kirino") [memory.go]
              → memory/characters/kirino/ dir created
              → memoryFile = memory/characters/kirino/MEMORY.md
```

---

## 验证结果

| 测试项 | 结果 |
|--------|------|
| `POST /character/clear?name=Kirino&id=kirino` | ✅ 200 → `{"character_id":"kirino","status":"ok"}` |
| `GET /character/active` | ✅ `{"character_id":"kirino"}` |
| 回退机制（仅 `name` 无 `id`） | ✅ name 值作为 id 使用 |
| 重置到默认 `id=picoclaw` | ✅ |
| `memory/characters/kirino/` 目录 | ✅ 已创建（空，首次写入生成 MEMORY.md） |
| `memory/characters/picoclaw/` 目录 | ✅ 已创建 |
| 旧 `memory/MEMORY.md` (3088 bytes) | ✅ 未丢失 |

---

## 注意事项

1. **旧记忆孤立**: 修复前默认角色的记忆在 `memory/MEMORY.md`（3088 bytes），
   修复后默认角色（`id=picoclaw`）的新记忆会写入 `memory/characters/picoclaw/MEMORY.md`。
   可通过一次 `MigrateFromDefault("picoclaw")` 迁移，但**不阻塞功能**。

2. **`/character/clear` 无 Bearer 认证**: 仅 WebUI 本地使用，与需要 token 的
   `POST /character/active` 不同。如果未来需要从外部访问此端点，需加 auth。

3. **JSONL 会话历史隔离**: 由 `session.dimensions`（`["chat","character"]`）独立保障，
   与 MEMORY.md 隔离是两套系统，互补不冲突。

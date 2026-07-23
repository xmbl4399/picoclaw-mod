# PicoClaw 魔改日志 — 2026-07-19

## 一、LuCI 聊天页

**文件**: `/usr/lib/lua/luci/view/admin_picoclaw/chat.htm` (384 bytes)

从之前的全屏暴力覆盖，改为保留 LuCI 左侧菜单和顶部 Tab 栏，聊天区填充右面板。

```html
<iframe id="iframeRoleplay" src="<%=gw_url%>/"></iframe>
```

`gw_url` 通过 `bridge.gateway_base()` 动态获取。

---

## 二、Galgame WebUI

**文件**: `/tmp/picoclaw_webui.html` (~26KB)

基于原版 picoclaw 聊天界面深度修改：

### 架构
- WebSocket: `ws://10.0.0.1:18790/pico/ws` + 子协议 `token.picoclaw-router-2026`
- 消息格式: `{ type: "message.send/update/create", payload: { content, kind, model_name } }`

### 功能清单

| 功能 | 实现 |
|------|------|
| 角色栏 | 水平 pill 按钮，数据源 `GET /cgi-bin/picoclaw_chars` → `_registry.json` |
| 角色切换 | 读取 `characters/{id}.md` → `POST /character/clear?name=X` + `POST /cgi-bin/picoclaw_soul` → 写 SOUL.md |
| 恢复默认 | `POST /character/clear?name=PicoClaw` + 重置 SOUL.md |
| 设置面板 | 角色卡列表 + 删除 + PNG 导入 + SOUL 预览 + 恢复默认 |
| PNG 导入 | SillyTavern 兼容的 PNG chunk 解析器（支持 V2 chara / V3 ccv3） |
| 聊天历史 | localStorage 按角色隔离: `pico-history-{角色id}` |
| 选项按钮 | `【1】文本【1】` 格式自动渲染为可点击按钮，点击发送 |
| 全屏按钮 | 角色名右侧 "全屏" → `target="_blank"` 新窗口 |

### 前端渲染
- 用户消息: 右对齐紫色气泡 `#8b5cf6`
- AI 回复: 左对齐卡片样式，带角色名头部
- Markdown 渲染: headers、代码块、加粗、斜体、引用

---

## 三、CGI 端点（`/www/cgi-bin/`）

| 端点 | 方法 | 功能 |
|------|------|------|
| `picoclaw_chars` | GET | 返回 `_registry.json` |
| `picoclaw_char?id=X` | GET | 返回 `characters/{id}.md` |
| `picoclaw_create_char` | POST | 写 `characters/{id}.md` + SOUL.md |
| `picoclaw_delete_char` | GET | ~~sed 删除（已废弃）~~ |
| `picoclaw_soul` | POST | 覆写 SOUL.md |
| `picoclaw_save_registry` | POST | 覆写 `_registry.json`（JSON 处理在浏览器端） |

所有 CGI 带 `Access-Control-Allow-Origin: *` + `Access-Control-Allow-Methods: GET, POST, OPTIONS`。

---

## 四、踩坑记录

| 问题 | 原因 | 解决 |
|------|------|------|
| LuCI 模板 500 错误 | 文件经 Windows/SMB 传输带 BOM (EF BB BF) | 路由端 `sed -i '1s/^\xef\xbb\xbf//'` |
| WebSocket 401 | 需要子协议 `token.picoclaw-router-2026` | `new WebSocket(url, 'token.' + TOKEN)` |
| CORS 拦截 POST | CGI 不处理 OPTIONS preflight | 所有 CGI 头加 `Access-Control-Allow-Methods: GET, POST, OPTIONS` + OPTIONS 返回 200 |
| CGI Exec format error | Z: 盘文件带 CRLF | 路由端 `tr -d '\r'` 或只修 shebang 行 |
| Shell 脚本生成 | PowerShell 吃掉引号/`$` 变量 | 改用 `write_to_file` 直接写 Z: 盘 |
| sed 破坏 JSON | JSON 不是行格式 | 删除/创建操作改为浏览器端 JSON.parse → 修改 → JSON.stringify → POST save_registry |
| 角色名显示错误 | Go 返回 model_name 而非角色名 | 改用 WebUI 本地 `characters` 列表查 `activeCharId` |

---

## 五、文件清单

### 修改的文件
```
/usr/lib/lua/luci/view/admin_picoclaw/chat.htm          # LuCI 聊天页外壳
/tmp/picoclaw_webui.html                                  # Galgame WebUI
/www/cgi-bin/picoclaw_chars                               # 角色列表 CGI
/www/cgi-bin/picoclaw_char                                # 角色文件读取 CGI
/www/cgi-bin/picoclaw_create_char                         # 创建角色 CGI
/www/cgi-bin/picoclaw_soul                                # SOUL.md 写入 CGI
/www/cgi-bin/picoclaw_save_registry                       # Registry 写入 CGI
/root/.picoclaw/workspace/characters/_registry.json       # 角色注册表
```

### 备份文件
```
/tmp/picoclaw_webui.html.bak  (旧 60KB WebUI)
/usr/lib/lua/luci/view/admin_picoclaw/chat.htm.bug        # 出问题的版本
/usr/lib/lua/luci/view/admin_picoclaw/chat.htm.topbar      # 带顶栏的版本
```

---

## 六、AI 侧优化（路由器 AI 自行完成）

SOUL.md 改为双层结构：
```
[角色身份卡内容]

---

Reply Rules（永久段）
[三选项规则]
```

切换角色时保留下半段规则段，不被覆写。


---

---

# PicoClaw 魔改日志 — 2026-07-24

## 一、缓存命中率优化（Go 后端 3 项）

### 1. System Prompt Cache — epoch 快速路径

**文件**: `go_src/pkg/agent/context.go`

**问题**: 每次 `BuildSystemPromptWithCache()` 读命中都遍历所有源文件做 stat()，导致毫秒级延迟。

**解决**: 新增 `cacheEpoch` 递增版号 + `cachedEpochSnapshot`，`sourceFilesChangedLocked()` 先比较版号（纳秒级），一致则跳过 stat。

### 2. Summary 前缀拆独立缓存块

**文件**: `go_src/pkg/agent/context.go`

**问题**: Summary 的前缀文案（"CONTEXT_SUMMARY: ..."）每次重复发送，从不参与 LLM KV 缓存。

**解决**: 将固定前缀和摘要内容拆为两个 PromptPart：
- 前缀 → `PromptCacheEphemeral`（可被 Anthropic KV 缓存）
- 内容 → `PromptCacheNone`

### 3. FreshTail 改为 Token 比例计算

**文件**: `go_src/pkg/seahorse/short_constants.go`, `short_assembler.go`

**问题**: 固定 `FreshTailCount=32` 在不同上下文窗口下适配不均——8K 窗口占太多，200K 窗口占太少。

**解决**: 新增 `FreshTailTokenPercent = 20%`，组装器按 budget × 20% 累计最新消息 token 数，大窗口保护更多内容，小窗口自动收窄。

---

## 二、Galgame WebUI 修复（前端 4 项）

### 1. format 字段全链路贯通

**问题**: 服务端发 `payload.format: "gal"`，但前端接收→存储→渲染→持久化→恢复的全链路中都丢弃了这个字段。
- `addOrUpdateBot` 只存 `{id, role, content, modelName, time}`，无 `format`
- `renderMessages` 传 `m.kind` 而非 `m.format` 给 `renderContent`
- `loadHistory` 恢复时也不恢复 `format`

**修复**: 全部贯通。消息对象统一加 `format`，渲染传 `m.format || m.kind`。

### 2. @keyframes fadeIn 缺失

**问题**: CSS 引用了 `animation: fadeIn` 但 `<style>` 块中没有定义该 `@keyframes`。

**修复**: 在 `</style>` 前补全动画定义。

### 3. 选项按钮防连点

**问题**: `sendChoice()` 只填输入框+发送，不禁止已点击按钮，连点会多次发送。

**修复**: 在 `sendChoice` 开头加 `document.querySelectorAll('.gal-option').forEach(b => b.disabled = true)`。

### 4. handleStreamEnd 未设 format

**问题**: 流式结束时 `ex.content = galBuffer[id]` 后未设 `ex.format = 'gal'`。

**修复**: 在 stream_end 逻辑补 `ex.format = 'gal'`。

---

## 三、跨平台交叉编译

**背景**: 原始 `go_src/pkg/` 是 `github.com/sipeed/picoclaw` 的 drop-in 替换。完整编译需要原始 module 的 `cmd/picoclaw/main.go`。

**解决**: 从 Go module cache 获取原始 module `v0.3.1`，替换 `pkg/` 目录后交叉编译：
```
GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -trimpath -ldflags="-s -w" -o /tmp/picoclaw_new_mipsle ./cmd/picoclaw
```
输出 32.8MB mipsle 二进制。

## 四、部署

```
10.0.0.1 (OpenWrt)
  ├─ /mnt/sda1/picoclaw_new          (新二进制 32.8MB)
  ├─ /tmp/picoclaw_bin                (运行中的 binary)
  └─ /tmp/picoclaw_webui.html         (修复后的 GAL WebUI)
PID 13150 运行中。
```

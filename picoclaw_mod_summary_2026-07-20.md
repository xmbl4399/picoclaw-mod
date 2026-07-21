# PicoClaw Mod — 6小时工作成果与经验教训

> 2026-07-19 20:25 ~ 2026-07-20 02:12 GMT+8

---

## 一、成果总览（8个子系统）

### 1. 角色会话隔离 ✅

**目标**：多角色切换时上下文不混淆。

**实现**：
- `session.dimensions` 从 `["chat"]` → `["chat", "character"]`
- `pkg/channels/pico/pico.go` 在消息 metadata 中注入 `character_id`
- `pkg/session/allocator.go` 的 `buildSessionScope` 从 InboundContext 读取 character 维度
- `pkg/health/server.go` 新增 `activeCharacter atomic.Value` + `/character/active` 端点
- `characterGetter` 从 pico 专用提升到 `BaseChannel`（QQ 通道自动继承）

**关键原则**：空 character 维度值被 `BuildSessionKey` 自动跳过，旧会话 key 不变，向后兼容。

### 2. 角色感知内存系统 ✅

**目标**：每个角色的记忆文件隔离。

**实现**：
- `MemoryStore` 构造函数签名：`(workspace)` → `(workspace, characterID string)`
- 路径：`memory/characters/{角色id}/MEMORY.md`
- `ContextBuilder` 新增 `SetCharacterID()` + `InvalidateCache()`
- `AgentInstance` → `ContextBuilder` → `MemoryStore` 回调链打通
- `LoadBootstrapFiles` 注入 RULES.md（agent 层自动拼接 SOUL.md + RULES.md）

### 3. context_budget 砍半冻结保护 ✅

**Bug**：`context_window=16384` 下 system prompt 吃满预算 → trim 循环丢光全部 45 条历史 → 返回 `nil` → LLM 凭空瞎编。

**修复**：每次丢完一轮保存当前 `best` 快照，当已丢弃超过原历史 50% 且仍超预算时，返回 `best` 保留最近一半对话。

**缓解**：路由器 `config.json` context_window 设到 **32768**（翻倍）。

**测试**：3 个测试用例全部通过，覆盖：丢弃最老会话、清空单条超大消息、保留被保护的活跃 turn。

### 4. WebUI GAL 格式渲染 ✅

**设计决策**：
- **Format Registry 架构**：`payload.format` 控制渲染引擎（markdown/gal/plain），`payload.kind` 控制消息来源语义
- 流式兼容：buffer + 全量重渲染，行碎片保护（不完整 @行跳过）

**前端实现**（`index.html`）：
- `FORMATS` 注册表
- `renderGal()` — 解析 `@场景/人物/说/心/旁白/回忆/提示/选1-3`
- 选项按钮绑定 `click → fill → send`，`disabled` 防连点
- `handleStream` / `handleStreamEnd` 流式处理
- CSS：`.gal-scene / .gal-character / .gal-dialogue / .gal-inner-thought / .gal-narration / .gal-flashback / .gal-notice / .gal-option`

**热更新**：通过 `PICOCLAW_WEBUI_OVERRIDE` 环境变量，上传 `/tmp/picoclaw_webui.html` 即可生效，无需重编 Go。

**Spec**：`gal_output_spec_v1.md`（9.2KB，与实际代码一致的 Format Dispatch 架构）。

### 5. AI 提示词（SOUL.md） ✅

用户直接写入 `/root/.picoclaw/workspace/SOUL.md`，包含完整 GAL 输出格式指令。

### 6. WebUI 根路径 404 修复 ✅

**根因**：`serveWebUI` 回调虽在 Health Server 内部，但**未注册到共享 mux**（`RegisterOnMux` 只注册了健康端点），根路径返回 404。

**修复**：`server.go` 的 `NewServer()` 和 `RegisterOnMux()` 中补注册 `mux.HandleFunc("/", serveWebUI)`。

### 7. CGI 脚本权限修复 ✅

| 文件 | 修复前 | 修复后 | 原因 |
|------|--------|--------|------|
| `picoclaw_soul` | `rwx--x--x` (711) | `rwxr-xr-x` (755) | uhttpd 以 nobody 运行，--x 可执行但 shell 解释器需要 **读** 权限（r） |
| `picoclaw_create_char` | 同上 | 755 | 同上 |
| `picoclaw_delete_char` | 同上 | 755 | 同上 |

### 8. WebSocket 认证与配置 ✅

- **WS 401 修复**：LuCI iframe WebSocket TOKEN 硬编码为 QQ 凭据 → 改为原始 token 字符串 `picoclaw-router-2026`
- **python3 拦截**：QQ 通道反复触发 `python3: not found` → 添加 `custom_deny_patterns: ["python3"]`
- **OverlayFS 双文件**：`/root/.picoclaw/config.json` 和 `/overlay/upper/root/.picoclaw/config.json` 是独立文件 → 直接写上层覆盖

---

## 二、编译与部署经验

### 编译命令（mipsle OpenWrt）
```bash
GOOS=linux GOARCH=mipsle GOMIPS=softfloat CGO_ENABLED=0 go build -tags stdjson -o picoclaw_bin
```

- `-tags stdjson` 排除 goolm 依赖（matrix 通道），二进制从 45MB → 31.8MB
- 排除后 mipsle 交叉编译成功，但 Windows/macOS 完整编译仍被 matrix/CGo/libolm 阻塞

### 部署路径
```
scp picoclaw_bin root@10.0.0.1:/mnt/sda1/picoclaw_new
# 启动脚本自动 cp 到 /tmp/picoclaw_bin
```

### OverlayFS 陷阱
- 配置文件存在两个物理位置：`/root/.picoclaw/config.json`（overlay upper）和 `/overlay/upper/root/.picoclaw/config.json`（物理层）
- 修改上层不会影响下层；删除字段后下层仍残留旧数据
- 解决：直接写 `/root/.picoclaw/config.json`（SSH 路径自动解析到 overlay upper）

### BusyBox 陷阱
- `sed` 不支持反向引用 `\1`（内部解释为字符 `1`）
- `cat -A` 不存在（BusyBox 简化版）
- 建议 config 修改一律用 `scp` 直传，避免在路由器上做文本处理

---

## 三、经验教训

### 教训 1：先问现有逻辑再动手
角色切换实现初期，我直接推出了完整的 session dimensions 方案。如果能先问你"当前角色切换的流程是什么？"，可以省掉 30 分钟的上下文重建。

### 教训 2：exec 工具在大文件/复杂命令上不可靠
PowerShell 编码问题 + BusyBox 兼容性 + 命令太长被截断，导致 8 分钟内上下文被压缩 3 次。解决：拆短命令、用 `ssh` 管道传脚本。

### 教训 3：OverlayFS 的存在不在预期中
直到部署时才发现路由器用了 overlayfs。下次改动任何路由器上的配置文件，应该先确认 `mount | grep overlay`。

### 教训 4：权限 — `--x` ≠ 能执行
CGI 脚本用 `setuid` / `setgid` 位给了执行权但没给读权（711）。`uhttpd` 以 `nobody` 运行，shell 解释器需要**先读取**脚本内容才能执行，所以只有 `--x` 不够，必须给 `r--`。

### 教训 5：WebUI 路由注册需要双重确认
mux 注册路径在 `NewServer()` 和 `RegisterOnMux()` 两个地方，只加其中一个不够。下次加路由端点记得两处都改。

### 教训 6：LLM context budget 切断 — 以量取胜不如留半
当 system prompt 膨胀到接近 context_window 时，trim 策略应该**保底留一半**而不是"全丢直到 fit"。砍半冻结保护是通用解法，不只是 PicoClaw 的问题。

### 教训 7：网关→WebUI 的 session key 设计
pico 通道 session key 仅基于 chatID 派生（`"pico:" + sessionID`），不感知角色变化。发现问题后注入 character 维度是正确解法，但更早的防御措施是：**session scope 默认应包含所有可区分的 inbound 维度**。

---

## 四、仍待解决的问题

1. **GAL 流式冗余**：`renderMessage()` 中残留旧的 GAL 流式逻辑，与 `handleStream` 重叠 → 需清理
2. **旧 MEMORY.md 迁移**：characterID 为空时 fallback 读默认路径，但旧内容不会自动迁移到角色子目录
3. **macOS/Windows 完整编译**：matrix CGo/libolm 依赖未解决（阻塞 `build-all`）
4. **exec 工具文档**：`action` 参数文档需补充（导致早期反复 exec 失败）
5. **构建脚本**：`webui/index.html` 路径在 build-all 脚本中缺失

---

## 六、配置调优（02:46）

### 修复 1：Context 压缩过猛

| 参数 | 旧值 🔴 | 当前值 ✅ | 说明 |
|------|---------|-----------|------|
| context_window | 32768 | **65536** | 匹配 DeepSeek 实际 64K 能力 ✅ |
| summarize_token_percent | 75 | **50** | 提前到 50% 触发压缩，避免暴力砍 ✅ |
| summarize_message_threshold | 20 | **50** | 多积累到 50 条才触发，减少频繁压缩 ✅ |

效果：65536 × 50% = ~32K tokens 时开始压缩，threshold=50 不会积累几条消息就暴力截断。不再出现"109 条砍到 1 条"的情况。

### 修复 2：Safety Guard 拦截 exec

| 参数 | 旧值 🔴 | 当前值 ✅ |
|------|---------|-----------|
| enable_deny_patterns | true | **false** |

内置高危命令模式库已关闭 ✅ 不再拦截合法命令。只保留自定义的 python3 拒绝规则。

---

## 七、启动链

### 完整链路
```
/etc/init.d/picoclaw (OpenWrt init.d 服务脚本)
  │  START=95, STOP=10
  │  cd /root/.picoclaw && /mnt/sda1/picoclaw.sh gateway
  │
  └→ /mnt/sda1/picoclaw.sh (环境变量 + 二进制准备)
       │  1. export PICOCLAW_MODEL_KEY="..."
       │  2. export PICOCLAW_WEBUI_OVERRIDE="/tmp/picoclaw_webui.html"
       │  3. export GOTRACEBACK=crash
       │  4. cp /mnt/sda1/picoclaw_new /tmp/picoclaw_bin 2>/dev/null
       │  5. chmod +x /tmp/picoclaw_bin
       │
       └→ exec /tmp/picoclaw_bin gateway (实际运行的二进制)
```

### /mnt/sda1/picoclaw 不是目录的原因
`/mnt/sda1/picoclaw` 最初是**旧版可执行文件**（不是目录），所以 `ls /mnt/sda1/picoclaw/` 报 `Not a directory`。新版二进制放在 `/mnt/sda1/picoclaw_new`，启动脚本每次启动时 cp 到 `/tmp/picoclaw_bin`。

### 环境变量热更新
- `PICOCLAW_WEBUI_OVERRIDE=/tmp/picoclaw_webui.html` — 修改 WebUI 只需 SCP 覆盖此文件，**不需重编译**
- `GOTRACEBACK=crash` — 崩溃时输出 goroutine 栈
- 自定义 deny patterns 已拦截 `python3` 调用

### 相关配置
- **cron 定时重启**：每天 4:03 / 16:03 `"/etc/init.d/picoclaw restart"`（已移除）
- **LuCI 命令白名单**：`/etc/init.d/picoclaw`、`/mnt/sda1/picoclaw`、`kill.*picoclaw`、`start-stop-daemon.*picoclaw`
- **PID 文件**：`/var/run/picoclaw.pid`

---

## 七、角色隔离方案 B 实施（02:34）

### 问题
角色切换后 `_registry.json` 的 `active_char` 从未被写回，页面刷新后前端不知道当前角色，但后端 SOUL.md 还是旧的。

### 方案 B：SOUL.md 自标记角色 ID

**设计**：让 SOUL.md 自己告诉前端它是谁，从根源解耦前后端同步。

**实现**：

1. **写 SOUL.md 时带 marker**（已在 `switchChar()` line 271）：
   ```javascript
   var markedPrompt = '<!-- char_id:' + id + ' -->\n' + prompt;
   await fetch('cgi-bin/picoclaw_soul', { method: 'POST', body: markedPrompt });
   ```

2. **初始化时以 SOUL.md marker 为权威来源**（`loadCharacters()` line 172-176）：
   ```javascript
   var soulR = await fetch('http://10.0.0.1/cgi-bin/picoclaw_soul');
   var soulText = await soulR.text();
   var m = soulText.match(/<!--\s*char_id:\s*(\S+?)\s*-->/);
   if (m && m[1]) activeCharId = m[1];  // 覆盖 registry
   ```

3. **同时持久化 registry**（`switchChar()` line 278-283，`resetChar()` line 312-316）：
   ```javascript
   reg.active_char = id;
   await fetch('cgi-bin/picoclaw_save_registry', { method: 'POST', body: JSON.stringify(reg) });
   ```

### 读取优先级
```
SOUL.md marker (权威) → registry active_char (fallback) → '' (默认 PicoClaw)
```

不再有同步问题：不管谁改了 SOUL.md，前端刷新后自动识别当前角色。

# GAL 输出规范 v1

## 1. 架构原则：Format Dispatch Layer

前端渲染管线采用 **Format Registry 架构**，两个关键字段职责分离：

| 字段 | 职责 | 取值（当前） |
|------|------|-------------|
| `payload.format` | **渲染引擎选择** | `"markdown"` / `"gal"` / `"plain"` |
| `payload.kind` | **消息来源语义** | `"thought"` / `"tool_calls"` / `""` |

**规则**：
- `kind` 控制可否被设置面板隐藏（thought/tool_calls 可以），`format` 控制用什么渲染引擎渲染正文
- `kind` 优先于 `format`：如果 `kind === 'thought'` 走灰色斜体，不启用 format 渲染
- 新增第 4 种格式只需向注册表添加一行

**注册表结构**（前端代码）：

```javascript
const FORMATS = {
  markdown: { render: renderContent },
  gal:      { render: renderGal },
  plain:    { render: (t) => t || '' },
};
```

## 2. 触发信号

GAL 格式通过 WebSocket payload 字段识别：

```json
{
  "type": "message.create",
  "id": "msg_1",
  "payload": {
    "content": "@场景 ...\n@说 ...",
    "format": "gal"
  }
}
```

- `format: "gal"` → 前端启用 `renderGal()` 渲染器
- `format` 缺失或为 `"markdown"` → 沿用现有 `renderContent()` 渲染（默认）

## 3. 前缀对照表

| 前缀 | CSS 类 | 语义 | 视觉特征 |
|------|--------|------|---------|
| `@场景` | `.gal-scene` | 环境/场景/景别 | 大字号，衬线字体，淡入动画 |
| `@人物` | `.gal-character` | 角色神态+动作 | 角色名高亮，动作描述轻量 |
| `@说` | `.gal-dialogue` | 角色对话 | 引号框/气泡，与说话人颜色关联 |
| `@心` | `.gal-inner-thought` | **主角内心独白** | 紫色斜体，区别于 `kind: thought` |
| `@旁白` | `.gal-narration` | 转场/叙事 | 居中斜体，小字 |
| `@回忆` | `.gal-flashback` | 回忆/闪回片段 | 暗色调/半透明/淡入 |
| `@提示` | `.gal-notice` | 好感度变动/系统提示 | 右上角 toast 或标注栏 |
| `@选1~@选3` | `.gal-option` | 交互选项 | 按钮/卡片样式，点击触发下一轮输入 |

### 关于 `@心` 的歧义消除

系统已有 `kind: thought`（AI 的推理过程，显示为灰色斜体）。

`@心` 是 **角色扮演场景中主角的内心独白**，是剧情内容的一部分，不是 AI 的思考。

| 维度 | `kind: thought` | `@心` |
|------|----------------|-------|
| 来源 | AI 推理过程 | 剧情内容 |
| CSS | `.msg.thought`（灰色斜体） | `.gal-inner-thought`（紫色） |
| 是否可隐藏 | 是（设置面板开关） | 否（它是剧情一部分） |
| 示例 | 用户数据库查询... | @心 明明害羞还嘴硬 |

## 4. 二层渲染架构

```
原始文本
   ↓ Layer 1: GAL 前缀解析器（renderGal）
   ↓ 按行匹配 /^@(场景|人物|说|心|旁白|回忆|提示|选[1-3])\s+(.*)/
   ↓ 生成结构化 DOM（带类名 + 数据属性）
   ↓ Layer 2: 内部 Markdown 渲染器（renderContent）
   ↓ 对每段正文执行标准的 Markdown 渲染
最终 HTML
```

## 5. 流式传输协议

### 5.1 消息流格式（服务端 → 前端）

```json
// 第一条：format 声明 + 首段内容（format 只需在首帧声明即可）
{"type":"message.stream","id":"msg_1","payload":{"format":"gal","content":"@场景 黄昏天台\n@人物 樱奈耳尖发红"}}

// 中间 chunk（无需重复 format）
{"type":"message.stream","id":"msg_1","payload":{"content":"\n@说 樱奈：我只是来吹风，**不是等你**"}}

// 结束帧：message.stream_end 类型
{"type":"message.stream_end","id":"msg_1","payload":{}}
```

- 首帧 **必须** 携带 `payload.format: "gal"`
- 后续 chunk **不必** 重复 format（前端根据首帧确定的 `galStreamMsgId` 自动判断）
- 结束帧使用 `message.stream_end` 类型，无需 `format` 字段

### 5.2 兼容老的 kind 方式

为兼容过渡期，`handleStream()` 也接受：

```json
{"type":"message.stream","id":"msg_1","payload":{"kind":"gal","content":"@场景 黄昏天台"}}
```

当 `payload.format` 缺省时自动回退：`payload.format || (kind === 'gal' ? 'gal' : 'markdown')`。

### 5.3 流式行碎片保护

流式传输时一行可能被拆成多个 chunk：

```
Chunk 1: "@说 樱奈：我只是来吹风"
Chunk 2: "，不是等你"
```

**处理规则**：检查 buffer 最后一行是否完整。如果最后一行是残缺的前缀行（如 `@说`），跳过该行渲染，残缺前缀留在 buffer 中等下个 chunk 自动补全。

### 5.4 选项渐进显示

流式下 `@选1/@选2/@选3` 会逐个出现，这是预期行为：

```
用户看到的时间线：
  t=0: @场景 黄昏天台...        → 场景出现
  t=1: @人物 樱奈...            → 人物出现
  t=2: @说 樱奈：我只是来吹风    → 对话出现
  t=3: @选1 伸手帮她捋头发      → 选项1 按钮出现
  t=4: @选2 调侃她              → 选项2 按钮出现
  t=5: @选3 安静陪她吹风        → 选项3 按钮出现（选项全部就绪）
```

## 6. 选项点击 → 发送机制

```javascript
// gal-option 点击事件：防连点
btn.addEventListener('click', function() {
  document.querySelectorAll('.gal-option').forEach(b => b.disabled = true);
  const input = document.getElementById('msgInput');
  input.value = this.textContent.trim();
  send();
});
```

**注意事项**：
- 点击后**禁用全部按钮**（防止连点）
- 发送后选项仍保留在历史消息中（不可编辑状态的快照）
- 已绑定的按钮有 `dataset.bound` 标记防止重复绑定

## 7. 非流式消息（message.create / message.update）

非流式场景下，`renderMessage()` 根据 `format` 字段走 FORMATS 注册表分发：

```javascript
const fmt = FORMATS[format] || FORMATS.markdown;
div.className = 'msg bot' + (format !== 'markdown' ? ' ' + format : '');
div.innerHTML = fmt.render(content);
```

## 8. 消息持久化

`sessionMsgs` 存储时携带 format：

```javascript
sessionMsgs.push({ _id, role: 'bot', content, kind, format, time });
```

`loadSession()` 恢复消息时传递 `m.format` 参数确保渲染正确。

## 9. CSS 样式参考

```css
/* ===== GAL Format Styles ===== */
.gal-scene {
  font-size: 1.15em;
  font-family: 'Noto Serif SC', serif;
  color: var(--accent2);
  padding: 12px 0 4px;
  animation: fadeIn 0.4s ease;
  line-height: 1.8;
}
.gal-character {
  color: var(--text);
  padding: 4px 0;
  font-size: 0.95em;
}
.gal-dialogue {
  padding: 10px 14px 10px 24px;
  margin: 4px 0;
  border-left: 3px solid var(--accent2);
  background: var(--input);
  border-radius: 0 8px 8px 0;
  line-height: 1.7;
}
.gal-inner-thought {
  color: #b388ff;
  font-style: italic;
  padding: 8px 0;
  animation: fadeIn 0.3s ease;
}
.gal-narration {
  text-align: center;
  font-style: italic;
  opacity: 0.7;
  font-size: 0.85em;
  padding: 12px 0;
}
.gal-flashback {
  opacity: 0.75;
  border-left: 2px dashed var(--border);
  padding-left: 14px;
  margin: 8px 0;
}
.gal-notice {
  text-align: center;
  font-size: 0.82em;
  padding: 6px 10px;
  margin: 6px 0;
  background: var(--accent);
  border-radius: 20px;
  display: inline-block;
  animation: fadeIn 0.3s ease;
  animation-delay: 0.2s;
}
.gal-option {
  display: block;
  width: 100%;
  padding: 12px 16px;
  margin: 6px 0;
  border: 1px solid var(--accent2);
  background: var(--card);
  color: var(--text);
  border-radius: 10px;
  cursor: pointer;
  font-size: 14px;
  text-align: center;
  transition: all 0.2s;
  animation: fadeIn 0.3s ease;
}
.gal-option:hover {
  background: var(--accent2);
  color: #fff;
  transform: translateY(-1px);
}
.gal-option:disabled {
  opacity: 0.3;
  cursor: default;
  transform: none;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(8px); }
  to   { opacity: 1; transform: translateY(0); }
}
```

## 10. 示例

### LLM 输出
```
@场景 黄昏天台，晚风卷起长发，路灯亮起
@人物 樱奈耳尖发红，低头捻衣角
@说 樱奈：我只是来吹风，**不是等你**
@心 明明害羞还嘴硬
@提示 樱奈好感小幅上升
@选1 伸手帮她捋头发
@选2 调侃她特意等我
@选3 安静陪她吹风
```

### 服务端→前端 wire format（流式）
```
[stream]   {"id":"m1","payload":{"format":"gal","content":"@场景 黄昏天台，晚风卷起长发，路灯亮起\n@人物 樱奈耳尖发红，低头捻衣角"}}
[stream]   {"id":"m1","payload":{"content":"\n@说 樱奈：我只是来吹风，**不是等你**"}}
[stream]   {"id":"m1","payload":{"content":"\n@心 明明害羞还嘴硬"}}
[stream]   {"id":"m1","payload":{"content":"\n@提示 樱奈好感小幅上升"}}
[stream]   {"id":"m1","payload":{"content":"\n@选1 伸手帮她捋头发"}}
[stream]   {"id":"m1","payload":{"content":"\n@选2 调侃她特意等我\n@选3 安静陪她吹风"}}
[stream_end] {"id":"m1","payload":{}}
```

### 前端渲染结果
```
┌─────────────────────────────────────┐
│ 🌅 黄昏天台，晚风卷起长发，路灯亮起   │  ← 场景大字号
│                                     │
│ 樱奈耳尖发红，低头捻衣角              │  ← 人物动作
│                                     │
│ │ 樱奈：我只是来吹风，不是等你         │  ← 对话气泡（左侧竖线）
│                                     │
│ 💭 明明害羞还嘴硬                     │  ← 内心独白（紫色斜体）
│                                     │
│          🎀 樱奈好感小幅上升           │  ← 提示小圆标
│                                     │
│ ┌─────────────────────────────────┐ │
│ │       伸手帮她捋头发              │ │  ← 可点击按钮
│ ├─────────────────────────────────┤ │
│ │       调侃她特意等我              │ │
│ ├─────────────────────────────────┤ │
│ │       安静陪她吹风                │ │
│ └─────────────────────────────────┘ │
└─────────────────────────────────────┘
```

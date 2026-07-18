# PicoClaw 魔改版 — AI 辅助部署指南

把 picoclaw 变成路由器/嵌入式设备上的角色扮演 AI 助手，全程由 AI 帮你搞定。

## 仓库结构

```
picoclaw-mod/
├── go_src/pkg/          # 全部 Go 源码魔改（10 个 .go + 1 个 .html）
├── luci/                # LuCI 面板魔改文件（6 个）
├── scripts/             # 启动脚本（picoclaw.sh + init.picoclaw）
├── workspace/           # 工作区文件（AGENT.md + SOUL.md + USER.md）
└── README.md            # 本文件
```

## 环境要求

- Go 1.26+
- OpenWrt/Kwrt 路由器或任意 Linux 设备
- 开发机：Windows / macOS / Linux 均可（交叉编译无需目标设备架构）
- SSH 访问目标设备

### 开始前请准备好

| 项目 | 说明 | 示例 |
|------|------|------|
| 路由器 IP | 你的路由器/目标设备地址 | `10.0.0.1` |
| SSH 用户名 | 路由器 SSH 登录用户名 | `root` |
| SSH 密码 | 路由器 SSH 登录密码 | `你的密码` |
| API Key | LLM 服务商的 API 密钥 | `sk-...` |

> **教程中的 `root@10.0.0.1` 和 `/root/` 均为示例**，请替换为你自己的地址、用户名和密码。文件传输统一使用 `scp`，跨平台通用。
>
> 默认安装到路由器自带存储 `/root/`，如果插了 U 盘（如 `/mnt/sda1`），把教程中的 `/root/` 替换为 `/mnt/sda1/` 即可。

---

## 已知问题（请先阅读）

以下 5 项缺陷当前版本**明确无法修复**，使用前请知悉：

| # | 缺陷 | 原因 |
|---|------|------|
| 1 | `/models` 只返回有 `api_keys` 的模型 | 网关代码 `gateway.go:255` 的 `len(m.APIKeys) > 0` 过滤逻辑，无 key 的模型不显示是设计意图 |
| 2 | API Key 硬编码在 `picoclaw.sh` | `export PICOCLAW_MODEL_KEY="sk-..."` 明文写在启动脚本里，没有凭证管理页面 |
| 3 | WebSocket 直连无鉴权代理 | webchat 直接 `ws://10.0.0.1:18790/pico/ws` 连网关，token 是固定字符串，缺 launcher 代理层 |
| 4 | 通道页面只读 | LuCI `channels.htm` 明文写"直接编辑 config.json"，无法通过 UI 管理通道 |
| 5 | 无图片/文件上传 | webchat 不支持粘贴/拖拽图片，无法利用 picoclaw vision 能力 |

---

## 支持架构

本魔改包是纯 Go 源码 + Lua/HTML 模板，不绑定特定 CPU。以下为理论上支持的架构：

| 架构 | GOOS | GOARCH | 环境变量 | 验证状态 | 典型设备 |
|------|------|--------|---------|---------|---------|
| MIPS 小端序 (软浮点) | linux | mipsle | `GOMIPS=softfloat` | ✅ 已实测 | 大多数 OpenWrt 路由器 (MT7621 等) |
| MIPS 大端序 (软浮点) | linux | mips | `GOMIPS=softfloat` | ⚠️ 未验证 | 老旧 Broadcom 路由 |
| ARMv7 (ARM32) | linux | arm | `GOARM=7` | ⚠️ 未验证 | 树莓派 2/3、ARM 盒子 |
| ARMv8 (ARM64) | linux | arm64 | — | ⚠️ 未验证 | 树莓派 4/5、ARM64 软路由 |
| x86_64 | linux | amd64 | — | ⚠️ 未验证 | x86 软路由、普通 Linux 服务器 |
| RISC-V 64 | linux | riscv64 | — | ⚠️ 未验证 | 部分新路由器/开发板 |
| Windows | windows | amd64 | — | ⚠️ 未验证 | 本地开发/测试 |
| macOS (Apple Silicon) | darwin | arm64 | — | ⚠️ 未验证 | 本地开发/测试 |

> 目前仅在 **MIPS 小端序 (mipsle + softfloat)** 上实测通过，其他架构理论支持但未经验证。
>
> 编译时只需改 3 个环境变量，其他一切相同。把 `go build` 命令中的 `GOOS/GOARCH/GOMIPS` 换成上表对应值即可。

### 编译命令速查

```powershell
# MIPS 小端序（主流路由器）
$env:GOOS='linux'; $env:GOARCH='mipsle'; $env:GOMIPS='softfloat'; $env:CGO_ENABLED='0'
go build -ldflags="-s -w" -o picoclaw_mipsle ./cmd/picoclaw/

# ARM64（树莓派 4/5、ARM 软路由）
$env:GOOS='linux'; $env:GOARCH='arm64'; $env:CGO_ENABLED='0'
go build -ldflags="-s -w" -o picoclaw_arm64 ./cmd/picoclaw/

# x86_64（软路由/服务器/本地测试）
$env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
go build -ldflags="-s -w" -o picoclaw_amd64 ./cmd/picoclaw/
```

---

## 方式一：一键让 AI 安装（推荐）

用 CodeBuddy、WorkBuddy、QClaw 等 AI 助手打开本仓库目录，直接说：

> 请按 README.md 教程帮我部署 picoclaw 到路由器。先 clone 原版，再把 go_src/pkg/ 覆盖进去，交叉编译（我的设备是 mipsle/arm64/amd64），用 scp 传到路由器部署二进制和 LuCI，写入启动脚本，初始化工作区，最后启动验证。

AI 会自动完成全部步骤，无需手动操作。

---

## 方式二：分步指挥 AI

| 步骤 | 告诉 AI |
|------|--------|
| 克隆 | `git clone https://github.com/nicepkg/picoclaw.git 到当前目录` |
| 魔改 | `把 go_src/pkg/ 下所有文件覆盖到 picoclaw 目录对应位置` |
| 编译 | `在 picoclaw 目录交叉编译（告诉我你的设备架构），输出二进制` |
| 部署二进制 | `把编译好的二进制和 go_src/pkg/health/webui/index.html 部署到目标设备` |
| 部署 LuCI | `把 luci/ 下文件部署到路由器对应路径（见下方 LuCI 表格），SSH 重启 uhttpd` |
| 部署脚本 | `把 scripts/picoclaw.sh 和 scripts/init.picoclaw 写入目标设备` |
| 初始化工作区 | `创建 workspace 目录结构，写入 workspace/ 下的 AGENT.md、SOUL.md、USER.md` |
| 启动验证 | `启动 picoclaw，curl 验证 health 端点` |

---

## 方式三：纯 AI 安装（无需本仓库）

即使没有本仓库，把这份 README 丢给 AI 也能装：

> 请按 README.md 一步一步完成 picoclaw 安装和魔改。先 clone 原版，然后按魔改文件清单逐个修改 Go 源码，完成后编译部署。

AI 会读取每个文件的改动描述，自动 `replace_in_file` 修改源码。

---

## 手动安装参考

如果你想手动操作，以下是完整步骤（以 MIPS 小端序路由器为例）。

### 1. 克隆原版

```bash
git clone https://github.com/nicepkg/picoclaw.git
cd picoclaw
```

### 2. Go 源码魔改（10 个文件）

| # | 文件 | 改动 |
|---|------|------|
| 1 | `pkg/channels/qq/qq.go` | go-cqhttp 消息格式解析，群聊/私聊路由 |
| 2 | `pkg/audio/tts/mimo_tts.go` | 添加 Mimo TTS provider |
| 3 | `pkg/audio/tts/tts.go` | TTS provider 注册扩展 |
| 4 | `pkg/providers/factory_provider.go` | 自定义 provider（deepseek 等）|
| 5 | `pkg/agent/definition.go` | 自定义 agent 字段 |
| 6 | `pkg/config/config.go` | `AgentDefaults` 加 `CharacterPrompt string` |
| 7 | `pkg/agent/context.go` | ContextBuilder 加 characterPrompt，提示词注入，缓存失效 |
| 8 | `pkg/agent/instance.go` | 传递 `CharacterPrompt` 到 ContextBuilder |
| 9 | `pkg/health/server.go` | import 扩展，config 白名单，`characterClearHandler`，注册表管理，路由注册 |
| 10 | `pkg/health/webui.go` + `webui/index.html` | `PICOCLAW_WEBUI_OVERRIDE` 支持 + 完整魔改 Webchat |

### 3. 交叉编译

根据目标设备选择对应架构（见上方「支持架构」表格）。以 MIPS 小端序为例：

```powershell
$env:GOOS='linux'; $env:GOARCH='mipsle'; $env:GOMIPS='softfloat'; $env:CGO_ENABLED='0'
go build -ldflags="-s -w" -o picoclaw_final ./cmd/picoclaw/
```

### 4. 部署文件到路由器

```bash
# 停止旧进程
ssh root@10.0.0.1 "/etc/init.d/picoclaw stop"

# scp 传二进制和 Webchat 页面到路由器 /root/
scp picoclaw_final root@10.0.0.1:/root/picoclaw_new
scp pkg/health/webui/index.html root@10.0.0.1:/root/webui_final.htm

# 把 webui 复制到 /tmp 供 PICOCLAW_WEBUI_OVERRIDE 读取
ssh root@10.0.0.1 "cp /root/webui_final.htm /tmp/picoclaw_webui.html"
```

> 如果路由器插了 U 盘（如 `/mnt/sda1`），把上面的 `/root/` 替换为 `/mnt/sda1/` 即可，二进制放 U 盘更稳妥不占用闪存。

### 5. 部署 LuCI 面板

| 文件 | 路由器路径 |
|------|-----------|
| `status.htm` | `/usr/lib/lua/luci/view/admin_picoclaw/` |
| `chat.htm` | `/usr/lib/lua/luci/view/admin_picoclaw/` |
| `agent.lua` | `/usr/lib/lua/luci/model/cbi/picoclaw/` |
| `security.lua` | `/usr/lib/lua/luci/model/cbi/picoclaw/` |
| `controller.lua` | `/usr/lib/lua/luci/controller/admin/` |
| `picoclaw_bridge.lua` | `/usr/lib/lua/` |

```bash
# 逐一 scp 传 LuCI 文件
scp luci/status.htm        root@10.0.0.1:/usr/lib/lua/luci/view/admin_picoclaw/
scp luci/chat.htm          root@10.0.0.1:/usr/lib/lua/luci/view/admin_picoclaw/
scp luci/agent.lua         root@10.0.0.1:/usr/lib/lua/luci/model/cbi/picoclaw/
scp luci/security.lua      root@10.0.0.1:/usr/lib/lua/luci/model/cbi/picoclaw/
scp luci/controller.lua    root@10.0.0.1:/usr/lib/lua/luci/controller/admin/
scp luci/picoclaw_bridge.lua root@10.0.0.1:/usr/lib/lua/

# 重启 uhttpd 使 LuCI 生效
ssh root@10.0.0.1 "rm -rf /tmp/luci-* && /etc/init.d/uhttpd restart"
```

### 6. 启动脚本

**`/root/picoclaw.sh`：**
```sh
#!/bin/sh
export PICOCLAW_MODEL_KEY="sk-你的密钥"
export PICOCLAW_WEBUI_OVERRIDE="/tmp/picoclaw_webui.html"
export GOTRACEBACK=crash
cp /root/picoclaw_new /tmp/picoclaw_bin 2>/dev/null
chmod +x /tmp/picoclaw_bin
exec /tmp/picoclaw_bin "$@"
```

**`/etc/init.d/picoclaw`：**
```sh
#!/bin/sh /etc/rc.common
START=95; STOP=10
PROG=/root/picoclaw.sh
PIDFILE=/var/run/picoclaw.pid
CONFIG_DIR=/root/.picoclaw

start() {
    [ -f "$PIDFILE" ] && kill -0 $(cat "$PIDFILE") 2>/dev/null && return 0
    cd "$CONFIG_DIR" && $PROG gateway > /tmp/picoclaw.log 2>&1 &
    echo $! > "$PIDFILE"
}
stop() {
    [ -f "$PIDFILE" ] && kill $(cat "$PIDFILE") 2>/dev/null
    rm -f "$PIDFILE"
}
```

### 7. 初始化工作区

```bash
ssh root@10.0.0.1 "mkdir -p /root/.picoclaw/workspace/characters"
```

写入 `AGENT.md`（角色卡系统操作指令）、默认 `SOUL.md`、`USER.md`。

### 8. 启动验证

```bash
ssh root@10.0.0.1 "/etc/init.d/picoclaw start"
ssh root@10.0.0.1 "pidof picoclaw_bin"
curl http://10.0.0.1:18790/health
```

- Webchat：`http://10.0.0.1:18790/`
- 仪表盘：`http://10.0.0.1/cgi-bin/luci/admin/services/picoclaw/status`

---

## 魔改文件总览

| # | 文件 | 类型 |
|---|------|------|
| 1 | `pkg/channels/qq/qq.go` | Go |
| 2 | `pkg/audio/tts/mimo_tts.go` | Go |
| 3 | `pkg/audio/tts/tts.go` | Go |
| 4 | `pkg/providers/factory_provider.go` | Go |
| 5 | `pkg/agent/definition.go` | Go |
| 6 | `pkg/config/config.go` | Go |
| 7 | `pkg/agent/context.go` | Go |
| 8 | `pkg/agent/instance.go` | Go |
| 9 | `pkg/health/server.go` | Go |
| 10 | `pkg/health/webui.go` + `webui/index.html` | Go + HTML |
| 11 | `status.htm` | LuCI 模板 |
| 12 | `chat.htm` | LuCI 模板 |
| 13 | `agent.lua` | LuCI CBI |
| 14 | `security.lua` | LuCI CBI |
| 15 | `controller.lua` | LuCI 路由 |
| 16 | `picoclaw_bridge.lua` | LuCI 桥接 |
| 17 | `picoclaw.sh` | 启动脚本 |
| 18 | `/etc/init.d/picoclaw` | init 脚本 |
| 19 | `AGENT.md` | 工作区 |
| 20 | `SOUL.md` | 工作区 |

**Go 魔改 10 文件 + LuCI 魔改 6 文件 + 配置/脚本/工作区 4 文件 = 共 20 个文件。**

---

## 角色扮演功能

支持 SillyTavern/酒馆 PNG 角色卡导入（V1/V2/V3），自然语言管理角色库。

详见 [角色扮演模块方案](角色扮演模块方案.md)。

---

## 踩坑记录

| 问题 | 原因 | 解决 |
|------|------|------|
| LuCI 模板 "unfinished string" | PowerShell UTF8 输出带 BOM | 无 BOM 写入 |
| ucodebridge 拒绝大改 | `<% %>` 块结构敏感 | 最小增量修改 |
| bridge.lua 被损坏 | PowerShell 文本处理改动了转义 | 纯 Lua 文件只通过 write_to_file 写入 |
| character_prompt 未生效 | 配置加载路径复杂 | 改用 SOUL.md 原生引导文件机制 |

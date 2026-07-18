--[[
PicoClaw Agent 配置 — CBI 表单
]]
local m, s, o

m = Map("picoclaw", "PicoClaw - Agent 默认配置",
       "配置 Agent 的默认行为参数，修改后自动热重载。")

-- 同步 JSON → UCI
do
	local fs = require "nixio.fs"
	if fs.access("/root/.picoclaw/config.json") then
		require("picoclaw_bridge").sync_json_to_uci()
	end
end

s = m:section(TypedSection, "picoclaw", "模型 & 提供方")
s.anonymous = true; s.addremove = false

o = s:option(ListValue, "model_name", "默认模型")
o:value("", "— 自动检测")
do
	local ok, json = pcall(require("picoclaw_bridge").read_json)
	if ok and type(json) == "table" and type(json.model_list) == "table" then
		for _, model in ipairs(json.model_list) do
			local name = model.model_name or model.model or ""
			if name ~= "" then o:value(name) end
		end
	end
end

o = s:option(Value, "temperature", "Temperature",
             "控制回复的随机性 (0.0 = 确定性, 2.0 = 创造性)")
o.datatype = "float"; o.placeholder = "1.0"

o = s:option(ListValue, "thinking_level", "Thinking 等级",
             "控制模型推理深度")
o:value("", "默认 (跟随模型)")
o:value("off", "关闭")
o:value("low", "低")
o:value("medium", "中")
o:value("high", "高")
o:value("xhigh", "极高")
o:value("adaptive", "自适应")

o = s:option(Value, "context_window", "上下文窗口",
             "发送给模型的最大上下文 Token 数")
o.datatype = "uinteger"; o.placeholder = "131072"

s = m:section(TypedSection, "picoclaw", "执行参数")
s.anonymous = true; s.addremove = false

o = s:option(ListValue, "steering_mode", "运行模式",
            "控制 Agent 的并行工具调用行为")
o:value("one-at-a-time", "逐步执行 (推荐)")
o:value("all", "全速并行")

o = s:option(Value, "max_tool_iterations", "最大工具迭代次数",
             "每轮对话最多执行工具调用的轮数")
o.datatype = "uinteger"; o.placeholder = "50"

o = s:option(Value, "max_tokens", "最大输出 Token",
             "默认最大输出 Token 数")
o.datatype = "uinteger"; o.placeholder = "32768"

o = s:option(Value, "max_media_size", "最大文件大小 (MB)",
             "允许的最大媒体文件大小")
o.datatype = "uinteger"; o.placeholder = "20"

o = s:option(Value, "llm_retries", "LLM 重试次数",
             "API 调用失败后的最大重试次数")
o.datatype = "uinteger"; o.placeholder = "2"

o = s:option(Value, "llm_retry_backoff", "重试间隔 (秒)",
             "每次重试之间的等待秒数")
o.datatype = "uinteger"; o.placeholder = "2"

s = m:section(TypedSection, "picoclaw", "上下文管理")
s.anonymous = true; s.addremove = false

o = s:option(Value, "summarize_message_threshold", "摘要消息阈值",
             "触发上下文摘要的消息数量")
o.datatype = "uinteger"; o.placeholder = "20"

o = s:option(Value, "summarize_token_percent", "摘要 Token 占比",
             "分配给摘要的 Token 预算百分比")
o.datatype = "uinteger"; o.placeholder = "75"

s = m:section(TypedSection, "picoclaw", "工作区设置")
s.anonymous = true; s.addremove = false

o = s:option(Value, "workspace", "工作区路径",
             "Agent 的工作目录")
o.placeholder = "/root/.picoclaw/workspace"

o = s:option(Flag, "restrict_to_workspace", "限制在工作区内",
             "将文件操作限制在工作区目录内")
o.default = "1"

o = s:option(Flag, "allow_read_outside_workspace", "允许读取外部文件",
             "允许读取工作区外的文件")
o.default = "0"

o = s:option(Flag, "split_on_marker", "消息标记分割",
             "在标记标签处分割消息")
o.default = "0"

function m.on_commit(map)
	require("picoclaw_bridge").apply_and_reload("agent")
end

return m

--[[
PicoClaw 安全设置 — CBI 表单
]]
local m, s, o

m = Map("picoclaw", "PicoClaw - 安全设置",
       "配置工具权限和执行安全策略，修改后自动热重载。")

do
	local fs = require "nixio.fs"
	if fs.access("/root/.picoclaw/config.json") then
		require("picoclaw_bridge").sync_json_to_uci()
	end
end

s = m:section(TypedSection, "picoclaw", "文件访问控制")
s.anonymous = true; s.addremove = false

o = s:option(DynamicList, "allow_read_path", "允许读取的路径",
             "Agent 可以读取的目录路径（一行一个）")
o.placeholder = "/proc"

o = s:option(DynamicList, "allow_write_path", "允许写入的路径",
             "Agent 可以写入的目录路径（一行一个）")
o.placeholder = "/tmp"

o = s:option(DynamicList, "custom_allow_pattern", "命令白名单模式",
             "允许执行的命令正则表达式（一行一个）")
o.placeholder = "/etc/init.d/picoclaw"

o = s:option(DynamicList, "custom_deny_pattern", "命令黑名单模式",
             "禁止执行的命令正则表达式（一行一个）")
o.placeholder = "rm /"

o = s:option(Value, "exec_timeout", "命令执行超时 (秒)",
             "工具调用的最大执行时间")
o.datatype = "uinteger"; o.placeholder = "60"

o = s:option(Value, "max_read_file_size", "最大读取文件大小 (字节)",
             "Agent 可以读取的最大文件大小")
o.datatype = "uinteger"; o.placeholder = "65536"

function m.on_commit(map)
	require("picoclaw_bridge").apply_and_reload("security")
end

return m

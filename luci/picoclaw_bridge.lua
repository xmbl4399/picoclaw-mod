--[[
PicoClaw UCI ↔ JSON bridge
Reads config.json for display, writes to UCI for CBI forms, syncs back to config.json
]]
module("picoclaw_bridge", package.seeall)

local JSON_FILE = "/root/.picoclaw/config.json"
local UCI_NAME = "picoclaw"
local GATEWAY = "http://127.0.0.1:18790"

-- Read config.json
function read_json()
	local f = io.open(JSON_FILE, "r")
	if not f then return {} end
	local s = f:read("*all")
	f:close()
	local jsonc = require("luci.jsonc")
	local ok, d = pcall(jsonc.parse, s)
	return (ok and type(d) == "table") and d or {}
end

-- Write config.json
function write_json(d)
	local jsonc = require("luci.jsonc")
	local ok, s = pcall(jsonc.stringify, d)
	if not ok or not s then
		ok, s = pcall(jsonc.stringify, d)
	end
	if not ok or not s then return false end
	local f = io.open(JSON_FILE, "w")
	if not f then return false end
	f:write(s)
	f:close()
	return true
end

-- UCI get (single value)
function uci_get(section, option)
	local f = io.popen("uci -q get " .. UCI_NAME .. "." .. section .. "." .. option .. " 2>/dev/null")
	if not f then return nil end
	local val = f:read("*l")
	f:close()
	return (val and val ~= "") and val or nil
end

-- UCI get (list)
function uci_get_list(section, option)
	local f = io.popen("uci -q get " .. UCI_NAME .. "." .. section .. "." .. option .. " 2>/dev/null")
	if not f then return {} end
	local items = {}
	while true do
		local line = f:read("*l")
		if not line then break end
		if line ~= "" then items[#items + 1] = line end
	end
	f:close()
	return items
end

-- UCI get boolean
function uci_get_bool(section, option)
	local v = uci_get(section, option)
	return (v == "1" or v == "true" or v == "on")
end

-- UCI get number
function uci_get_num(section, option)
	local v = uci_get(section, option)
	return v and tonumber(v) or nil
end

-- Sync UCI agent section → config.json.agents.defaults
function sync_uci_agent()
	local json = read_json()
	if not json.agents then json.agents = {} end
	if not json.agents.defaults then json.agents.defaults = {} end
	local ad = json.agents.defaults

	local function set(k, v)
		if v ~= nil then ad[k] = v end
	end

	set("model_name", uci_get("agent", "model_name"))
	set("steering_mode", uci_get("agent", "steering_mode"))
	set("max_tool_iterations", uci_get_num("agent", "max_tool_iterations"))
	set("max_tokens", uci_get_num("agent", "max_tokens"))
	set("summarize_message_threshold", uci_get_num("agent", "summarize_message_threshold"))
	set("summarize_token_percent", uci_get_num("agent", "summarize_token_percent"))
	set("workspace", uci_get("agent", "workspace"))
	set("restrict_to_workspace", uci_get_bool("agent", "restrict_to_workspace"))
	set("allow_read_outside_workspace", uci_get_bool("agent", "allow_read_outside_workspace"))

	return write_json(json)
end

-- Sync UCI security section → config.json.tools
function sync_uci_security()
	local json = read_json()
	if not json.tools then json.tools = {} end
	local t = json.tools

	local rp = uci_get_list("security", "allow_read_path")
	if #rp > 0 then t.allow_read_paths = rp end

	local wp = uci_get_list("security", "allow_write_path")
	if #wp > 0 then t.allow_write_paths = wp end

	if not t.exec then t.exec = {} end
	local cap = uci_get_list("security", "custom_allow_pattern")
	if #cap > 0 then t.exec.custom_allow_patterns = cap end
	local cdp = uci_get_list("security", "custom_deny_pattern")
	if #cdp > 0 then t.exec.custom_deny_patterns = cdp end
	local et = uci_get_num("security", "exec_timeout")
	if et then t.exec.timeout_seconds = et end

	if not t.read_file then t.read_file = {} end
	local mrf = uci_get_num("security", "max_read_file_size")
	if mrf then t.read_file.max_read_file_size = mrf end

	return write_json(json)
end

-- Sync ALL UCI sections → config.json
function sync_uci_all()
	local ok1 = sync_uci_agent()
	local ok2 = sync_uci_security()
	return ok1 or ok2
end

-- Sync config.json → UCI
function sync_json_to_uci()
	local json = read_json()
	local ad = (json.agents or {}).defaults or {}
	local tools = json.tools or {}
	local exec = tools.exec or {}
	local rf = tools.read_file or {}

	-- Delete existing sections first
	os.execute("uci -q delete " .. UCI_NAME .. ".agent 2>/dev/null")
	os.execute("uci -q delete " .. UCI_NAME .. ".security 2>/dev/null")
	os.execute("uci -q delete " .. UCI_NAME .. ".model_info 2>/dev/null")

	-- Create agent section
	os.execute("uci set " .. UCI_NAME .. ".agent='picoclaw'")
	for k, v in pairs(ad) do
		local vt = type(v)
		if vt == "boolean" then v = v and "1" or "0" end
		if vt == "table" or vt == "function" or vt == "userdata" or vt == "thread" or vt == "nil" then
			-- skip complex types
		else
			local escaped = tostring(v):gsub("'", "'\\''")
			os.execute("uci set " .. UCI_NAME .. ".agent." .. k .. "='" .. escaped .. "'")
		end
	end

	-- Create security section
	os.execute("uci set " .. UCI_NAME .. ".security='picoclaw'")
	if type(tools.allow_read_paths) == "table" then
		for _, p in ipairs(tools.allow_read_paths) do
			os.execute("uci add_list " .. UCI_NAME .. ".security.allow_read_path='" .. p:gsub("'", "'\\''") .. "'")
		end
	end
	if type(tools.allow_write_paths) == "table" then
		for _, p in ipairs(tools.allow_write_paths) do
			os.execute("uci add_list " .. UCI_NAME .. ".security.allow_write_path='" .. p:gsub("'", "'\\''") .. "'")
		end
	end
	if type(exec.custom_allow_patterns) == "table" then
		for _, p in ipairs(exec.custom_allow_patterns) do
			os.execute("uci add_list " .. UCI_NAME .. ".security.custom_allow_pattern='" .. p:gsub("'", "'\\''") .. "'")
		end
	end
	if type(exec.custom_deny_patterns) == "table" then
		for _, p in ipairs(exec.custom_deny_patterns) do
			os.execute("uci add_list " .. UCI_NAME .. ".security.custom_deny_pattern='" .. p:gsub("'", "'\\''") .. "'")
		end
	end
	if exec.timeout_seconds then
		os.execute("uci set " .. UCI_NAME .. ".security.exec_timeout='" .. exec.timeout_seconds .. "'")
	end
	if rf.max_read_file_size then
		os.execute("uci set " .. UCI_NAME .. ".security.max_read_file_size='" .. rf.max_read_file_size .. "'")
	end

	-- Create model_info section (read-only summary for display)
	os.execute("uci set " .. UCI_NAME .. ".model_info='model_info'")
	os.execute("uci set " .. UCI_NAME .. ".model_info.count='" .. (#(json.model_list or {})) .. "'")
	local dm = (ad.model_name) or (json.model_list and json.model_list[1] and json.model_list[1].model_name) or ""
	os.execute("uci set " .. UCI_NAME .. ".model_info.default_model='" .. dm:gsub("'", "'\\''") .. "'")

	os.execute("uci commit " .. UCI_NAME)
end

-- Hot-reload PicoClaw via its /config API
function hot_reload()
	os.execute("curl -s -X POST --data-binary @" .. JSON_FILE .. " " .. GATEWAY .. "/config >/dev/null 2>&1 &")
end

-- Full sync: read UCI, write JSON, hot-reload
function apply_and_reload(section)
	if section == "agent" then
		local ok = sync_uci_agent()
		if ok then hot_reload() end
		return ok
	elseif section == "security" then
		local ok = sync_uci_security()
		if ok then hot_reload() end
		return ok
	end
	local ok = sync_uci_all()
	if ok then hot_reload() end
	return ok
end

-- Check if PicoClaw is running
function is_running()
	return (luci.sys.call("pidof picoclaw_bin >/dev/null 2>&1") == 0)
end

-- Get process info
function process_info()
	if not is_running() then return nil end
	local f = io.popen("pidof picoclaw_bin 2>/dev/null")
	if not f then return nil end
	local pid = tonumber(f:read("*l") or "0")
	f:close()
	if not pid or pid == 0 then return nil end
	
	-- Get uptime from /proc
	local ut = "unknown"
	local us = io.popen("stat -c %Y /proc/" .. pid .. " 2>/dev/null")
	if us then
		local boot_ts = tonumber(us:read("*l"))
		us:close()
		if boot_ts then
			local now = os.time()
			local diff = now - boot_ts
			local mins = math.floor(diff / 60)
			local hours = math.floor(mins / 60)
			local days = math.floor(hours / 24)
			if days > 0 then ut = days .. "d " .. (hours % 24) .. "h"
			elseif hours > 0 then ut = hours .. "h " .. (mins % 60) .. "m"
			else ut = mins .. "m"
			end
		end
	end
	
	return {pid = pid, uptime = ut}
end

-- Get LAN IP from UCI (fallback to 10.0.0.1)
function lan_ip()
	local f = io.popen("uci -q get network.lan.ipaddr 2>/dev/null | cut -d'/' -f1")
	if not f then return "10.0.0.1" end
	local ip = f:read("*l")
	f:close()
	return (ip and ip ~= "") and ip or "10.0.0.1"
end

-- Get gateway base URL for template use
function gateway_base()
	return "http://" .. lan_ip() .. ":18790"
end

-- Get gateway URL for WebSocket connections
function gateway_ws()
	return "ws://" .. lan_ip() .. ":18790/pico/ws"
end

-- ── Character Registry ──

local function workspace_dir()
	local json = read_json()
	local ad = (json.agents or {}).defaults or {}
	return (ad.workspace or "/root/.picoclaw/workspace")
end

-- Read character registry from workspace
function character_registry()
	local chars_dir = workspace_dir() .. "/characters"
	local reg_path = chars_dir .. "/_registry.json"
	local f = io.open(reg_path, "r")
	if not f then return {characters = {}, active_char = ""} end
	local s = f:read("*all")
	f:close()
	local jsonc = require("luci.jsonc")
	local ok, reg = pcall(jsonc.parse, s)
	if not ok or type(reg) ~= "table" then
		return {characters = {}, active_char = ""}
	end
	return reg
end

-- Read a single character prompt from its .md file
function character_prompt(char_id)
	char_id = char_id:gsub("[^a-zA-Z0-9%-_]", "")
	if char_id == "" then return "" end
	local f = io.open(workspace_dir() .. "/characters/" .. char_id .. ".md", "r")
	if not f then return "" end
	local s = f:read("*all")
	f:close()
	return s
end

-- Switch character and notify PicoClaw
function character_switch(char_id)
	local reg = character_registry()
	local name = ""
	local prompt = ""
	
	if char_id == "" then
		-- Reset to default
		prompt = "I am PicoClaw, a helpful AI assistant."
	else
		prompt = character_prompt(char_id)
		if prompt == "" then return false, "Character not found: " .. char_id end
		for _, c in ipairs(reg.characters or {}) do
			if c.id == char_id then
				name = c.name or ""
				break
			end
		end
	end
	
	-- POST to PicoClaw /character/clear
	local escaped_name = name:gsub("'", "'\\''")
	local escaped_prompt = prompt:gsub("'", "'\\''")
	local cmd = "curl -s -X POST http://127.0.0.1:18790/character/clear" ..
		" -d '" .. escaped_prompt .. "'" ..
		" 2>/dev/null &"
	os.execute(cmd)
	
	return true, "ok"
end

-- Render channel cards as HTML for channels.htm
function channel_cards()
  local cfg = read_json()
  local raw = cfg.channel_list or cfg.channels or {}
  local ch = {}
  for k, v in pairs(raw) do
    v._key = k
    ch[#ch + 1] = v
  end
  local function cmp(a, b)
    if a.enabled ~= b.enabled then return a.enabled end
    return (a._key or "") < (b._key or "")
  end
  table.sort(ch, cmp)

  local icons = {
    telegram = "✈️", discord = "💬", qq = "🐧",
    weixin = "💚", whatsapp = "📱", slack = "🛋",
    pico = "🦞", feishu = "📘", deltachat = "📧",
    dingtalk = "📌"
  }

  local parts = {}
  for _, c in ipairs(ch) do
    local key = c._key or "?"
    local platform = c.type or c.platform or key
    local enabled = c.enabled
    local ch_id = c.app_id or c.bot_id or c.id or key
    local icon = icons[platform] or "🔌"
    local bg = enabled and "rgba(55,164,71,.12)" or "rgba(204,68,68,.1)"
    local st_class = enabled and "on" or "off"
    local st_text = enabled and "已启用" or "已禁用"
    local st_dot = '<span class="status-dot ' .. st_class .. '"></span>'

    parts[#parts + 1] = '<div class="ch-card">' ..
      '<div class="ch-icon" style="background:' .. bg .. '">' .. icon .. '</div>' ..
      '<div class="ch-info">' ..
      '<div class="ch-name">' .. key .. '</div>' ..
      '<div class="ch-detail">' ..
      '<span>类型: <strong>' .. platform .. '</strong></span>' ..
      '<span>键: <code style="font-size:10.5px">' .. key .. '</code></span>' ..
      '<span>AppID: <code style="font-size:10.5px">' .. ch_id .. '</code></span>' ..
      '<span class="ch-status ' .. st_class .. '">' .. st_dot .. ' ' .. st_text .. '</span>' ..
      '</div></div></div>'
  end

  return #parts, table.concat(parts)
end

function channel_enabled_count()
  local cfg = read_json()
  local raw = cfg.channel_list or cfg.channels or {}
  local cnt = 0
  for _, v in pairs(raw) do
    if v.enabled then cnt = cnt + 1 end
  end
  return cnt
end

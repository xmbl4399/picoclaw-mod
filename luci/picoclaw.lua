module("luci.controller.admin.picoclaw", package.seeall)

require "picoclaw_bridge"
function index()

	entry({"admin", "services", "picoclaw"},
		alias("admin", "services", "picoclaw", "status"),
		_("PicoClaw AI"), 80)

	entry({"admin", "services", "picoclaw", "status"},
		template("admin_picoclaw/status"),
		_("仪表盘"), 10).leaf = true

	entry({"admin", "services", "picoclaw", "chat"},
		template("admin_picoclaw/chat"),
		_("聊天"), 20).leaf = true

	entry({"admin", "services", "picoclaw", "logs"},
		template("admin_picoclaw/logs"),
		_("日志"), 30).leaf = true

	entry({"admin", "services", "picoclaw", "channels"},
		template("admin_picoclaw/channels"),
		_("通道"), 40).leaf = true

	entry({"admin", "services", "picoclaw", "agent"},
		cbi("picoclaw/agent"),
		_("Agent 配置"), 50).leaf = true

	entry({"admin", "services", "picoclaw", "security"},
		cbi("picoclaw/security"),
		_("安全设置"), 60).leaf = true

	entry({"admin", "services", "picoclaw", "start"},   call("action_start"), nil)
	entry({"admin", "services", "picoclaw", "stop"},    call("action_stop"), nil)
	entry({"admin", "services", "picoclaw", "restart"}, call("action_restart"), nil)

	entry({"admin", "services", "picoclaw", "config_read"},  call("action_config_read"), nil).leaf = true
	entry({"admin", "services", "picoclaw", "health_check"}, call("action_health_check"), nil).leaf = true
	entry({"admin", "services", "picoclaw", "models_data"},  call("action_models_data"), nil).leaf = true
	entry({"admin", "services", "picoclaw", "status_data"},  call("action_status_data"), nil).leaf = true
	entry({"admin", "services", "picoclaw", "log_data"},     call("action_log_data"), nil).leaf = true
	entry({"admin", "services", "picoclaw", "proxy_api"},    call("action_proxy_api"), nil).leaf = true
	entry({"admin", "services", "picoclaw", "char_list"},   call("action_char_list"), nil).leaf = true
	entry({"admin", "services", "picoclaw", "char_switch"}, call("action_char_switch"), nil).leaf = true
end

local PICOCLAW_INIT = "/etc/init.d/picoclaw"

function action_start()
	os.execute(PICOCLAW_INIT .. " start &")
	luci.http.prepare_content("text/html; charset=utf-8")
	luci.http.write([[<html><head><meta charset="UTF-8"><meta http-equiv="refresh" content="1;url=]] .. luci.dispatcher.build_url("admin", "services", "picoclaw", "status") .. [["/></head><body style="font-family:sans-serif;text-align:center;padding-top:100px"><p>正在启动 PicoClaw...</p><p style="font-size:12px;color:#888">即将跳转到仪表盘</p></body></html>]])
end

function action_stop()
	os.execute(PICOCLAW_INIT .. " stop &")
	luci.http.prepare_content("text/html; charset=utf-8")
	luci.http.write([[<html><head><meta charset="UTF-8"><meta http-equiv="refresh" content="1;url=]] .. luci.dispatcher.build_url("admin", "services", "picoclaw", "status") .. [["/></head><body style="font-family:sans-serif;text-align:center;padding-top:100px"><p>正在停止 PicoClaw...</p><p style="font-size:12px;color:#888">即将跳转到仪表盘</p></body></html>]])
end

function action_restart()
	os.execute(PICOCLAW_INIT .. " restart &")
	luci.http.prepare_content("text/html; charset=utf-8")
	luci.http.write([[<html><head><meta charset="UTF-8"><meta http-equiv="refresh" content="1;url=]] .. luci.dispatcher.build_url("admin", "services", "picoclaw", "status") .. [["/></head><body style="font-family:sans-serif;text-align:center;padding-top:100px"><p>正在重启 PicoClaw...</p><p style="font-size:12px;color:#888">即将跳转到仪表盘</p></body></html>]])
end

function action_config_read()
	luci.http.prepare_content("application/json")
	local d = picoclaw_bridge.read_json()
	luci.http.write_json(d)
end

function action_health_check()
	local result = luci.sys.exec("curl -s --connect-timeout 3 http://127.0.0.1:18790/health 2>/dev/null")
	luci.http.prepare_content("application/json")
	if result and #result > 0 then
		luci.http.write(result)
	else
		luci.http.write_json({status = "error", error = "PicoClaw not reachable"})
	end
end

function action_models_data()
	local d = picoclaw_bridge.read_json()
	local models = d.model_list or {}
	local ad = (d.agents or {}).defaults or {}
	local default_model = ad.model_name or (models[1] and models[1].model_name) or ""
	luci.http.prepare_content("application/json")
	luci.http.write_json({models = models, default_model = default_model})
end

function action_status_data()
	local info = picoclaw_bridge.process_info()
	luci.http.prepare_content("application/json")
	local data = {}
	if info then
		data.running = true
		data.pid = info.pid
		data.uptime = info.uptime
		data.memory = info.memory
	else
		data.running = false
		data.pid = 0
		data.uptime = "—"
		data.memory = "—"
	end
	data.gateway_url = picoclaw_bridge.gateway_base()
	data.gateway_ws  = picoclaw_bridge.gateway_ws()

	local cfg = picoclaw_bridge.read_json()
	if cfg then
		data.build_version = (cfg.build_info or {}).version or "—"
		local ch = cfg.channel_list or {}
		local cnt = 0
		for _, v in pairs(ch) do if v.enabled then cnt = cnt + 1 end end
		data.channel_count = cnt
		data.channel_total = #ch
		local ad = (cfg.agents or {}).defaults or {}
		data.default_model = ad.model_name or (cfg.model_list and cfg.model_list[1] and cfg.model_list[1].model_name) or "—"
		data.model_count = #(cfg.model_list or {})
		data.temperature = ad.temperature
		data.context_window = ad.context_window
		data.thinking_level = ad.thinking_level
		data.steering_mode = ad.steering_mode
	end

	local health_result = luci.sys.exec("curl -s --connect-timeout 2 http://127.0.0.1:18790/health 2>/dev/null")
	if health_result and #health_result > 0 then
		local jsonc = require("luci.jsonc")
		local ok, hd = pcall(jsonc.parse, health_result)
		if ok and type(hd) == "table" then data.health = hd end
	end

	luci.http.write_json(data)
end

local function strip_ansi(s)
	if not s then return "" end
	s = s:gsub("\27%[.-m", "")
	s = s:gsub("\27%[.-%a", "")
	s = s:gsub("\27%].-\27\\", "")
	s = s:gsub("\27%[?%d+[hl]", "")
	s = s:gsub("\r\n", "\n")
	s = s:gsub("\r", "")
	return s
end

function action_log_data()
	luci.http.prepare_content("application/json; charset=utf-8")
	local count = tonumber(luci.http.formvalue("lines") or "100")
	if count > 500 then count = 500 end
	local result = luci.sys.exec("tail -" .. count .. " /tmp/picoclaw.log 2>/dev/null")
	local raw_lines = {}
	if result and #result > 0 then
		local cleaned = strip_ansi(result)
		for line in cleaned:gmatch("([^\n]+)") do
			raw_lines[#raw_lines + 1] = line
		end
	end
	local f = io.popen("wc -l /tmp/picoclaw.log 2>/dev/null | awk '{print $1}'")
	local total = f and tonumber(f:read("*l")) or 0
	if f then f:close() end
	luci.http.write_json({logs = raw_lines, total = total, truncated = total > count})
end

function action_proxy_api()
	local path = luci.http.formvalue("path") or "/"
	local allowed = {["/health"] = true, ["/config"] = true, ["/models"] = true, ["/"] = true}
	if not allowed[path] then
		luci.http.prepare_content("application/json")
		luci.http.write_json({status = "error", error = "path not allowed"})
		return
	end
	local result = luci.sys.exec("curl -s --connect-timeout 3 http://127.0.0.1:18790" .. path .. " 2>/dev/null")
	luci.http.prepare_content("application/json")
	if result and #result > 0 then
		luci.http.write(result)
	else
		luci.http.write_json({status = "error", error = "proxy failed"})
	end
end

function action_char_list()
	luci.http.prepare_content("application/json")
	local reg = picoclaw_bridge.character_registry()
	luci.http.write_json(reg)
end

function action_char_switch()
	luci.http.prepare_content("application/json")
	local char_id = luci.http.formvalue("id") or ""
	local ok, err = picoclaw_bridge.character_switch(char_id)
	if ok then
		luci.http.write_json({status = "ok"})
	else
		luci.http.write_json({status = "error", error = err or "unknown"})
	end
end

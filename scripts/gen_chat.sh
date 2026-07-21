#!/bin/sh
cat > /usr/lib/lua/luci/view/admin_picoclaw/chat.htm << 'EOF'
<%+header%>
<%
local bridge = require "picoclaw_bridge"
local gw_url = bridge.gateway_base()
%>
<style>
#maincontent, #maincontent > .container { margin:0!important; padding:0!important; max-width:none!important }
#iframeRoleplay { width:100%; height:calc(100vh - 100px); border:none; background:#09090b }
</style>
<iframe id="iframeRoleplay" src="<%=gw_url%>/"></iframe>
<%+footer%>
EOF
/etc/init.d/uhttpd restart 2>/dev/null
echo "chat.htm done: $(wc -c < /usr/lib/lua/luci/view/admin_picoclaw/chat.htm) bytes"

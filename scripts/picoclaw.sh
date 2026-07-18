#!/bin/sh
export PICOCLAW_MODEL_KEY="sk-在此填入你的API密钥"
export PICOCLAW_WEBUI_OVERRIDE="/tmp/picoclaw_webui.html"
export GOTRACEBACK=crash
cp /mnt/sda1/picoclaw_new /tmp/picoclaw_bin 2>/dev/null
chmod +x /tmp/picoclaw_bin
exec /tmp/picoclaw_bin "$@"
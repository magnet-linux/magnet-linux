#!/bin/sh
# Invoked by Hyprland and by D-Bus activation in the same login session.
set -eu
config=${XDG_CONFIG_HOME:-$HOME/.config}/mako/config
if ! test -f "$config"; then config=/etc/xdg/mako/config; fi
state=${XDG_STATE_HOME:-$HOME/.local/state}/magnet-linux
mkdir -p "$state"
exec mako --config "$config" "$@" >> "$state/notifications.log" 2>&1

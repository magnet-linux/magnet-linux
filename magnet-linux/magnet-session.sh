#!/bin/sh
# Hyprland session entry point; user files take precedence outside the greeter.
set -eu
session=hyprland
case "${1:-}" in
  hyprland) shift;;
  --greeter) session=greeter; shift;;
  '') ;;
  *) echo 'usage: magnet-session [hyprland|--greeter]' >&2; exit 2;;
esac
uid=$(id -u)
export XDG_RUNTIME_DIR=/run/user/"$uid"
test -d "$XDG_RUNTIME_DIR" && test ! -L "$XDG_RUNTIME_DIR" &&
  test "$(stat -c %u "$XDG_RUNTIME_DIR")" = "$uid" &&
  test "$(stat -c %a "$XDG_RUNTIME_DIR")" = 700 || {
    echo 'Desktop runtime directory is missing; run setup-desktop for this account.' >&2
    exit 1
  }
export XDG_SESSION_TYPE=wayland LIBSEAT_BACKEND=seatd LANG=C.UTF-8
export XDG_SESSION_DESKTOP=Hyprland
state=${XDG_STATE_HOME:-$HOME/.local/state}/magnet-linux
mkdir -p "$state"
umask 077
exec >> "$state/$session.log" 2>&1
export XDG_CURRENT_DESKTOP=Hyprland
export XCURSOR_THEME="${XCURSOR_THEME:-Adwaita}"
export XCURSOR_SIZE="${XCURSOR_SIZE:-24}"
if test "$session" = greeter; then
  config=/etc/greetd/hyprland.lua
  exec dbus-run-session -- start-hyprland -- --config "$config" "$@"
else
  config=${XDG_CONFIG_HOME:-$HOME/.config}/hypr/hyprland.lua
  if ! test -f "$config"; then config=/etc/xdg/hypr/hyprland.lua; fi
fi
exec dbus-run-session -- magnet-audio-session start-hyprland -- --config "$config" "$@"

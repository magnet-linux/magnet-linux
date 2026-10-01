#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
# Exercise graphical authentication and Hyprland through a private QEMU UI.
# shellcheck disable=SC2016,SC2029
set -euo pipefail
umask 077
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
vm=$(realpath "${1:-$repo/magnet-linux/out/hyprland-vm}")
user=$(cat "$vm/login-user")
test "$user" != root
test -f "$vm/graphical-login"
exec 9> "$vm/run.lock"
flock -n 9 || { echo 'The VM is already running.' >&2; exit 1; }
work=$(mktemp -d "$repo/magnet-linux/out/session-test.XXXXXXXX")
echo "Test artifacts: $work"
qemu_pid='' xvfb_pid='' http_pid=''
cleanup() {
  set +e
  if test -n "$qemu_pid" && kill -0 "$qemu_pid" 2>/dev/null && declare -F shutdown_guest >/dev/null; then
    shutdown_guest > /dev/null 2>&1
    for ((i=0;i<60;i++)); do
      kill -0 "$qemu_pid" 2>/dev/null || break
      sleep .2
    done
  fi
  for pid in "$qemu_pid" "$http_pid" "$xvfb_pid"; do
    if test -n "$pid"; then kill "$pid" 2>/dev/null; wait "$pid" 2>/dev/null; fi
  done
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
Xvfb -displayfd 3 -screen 0 1440x1000x24 -nolisten tcp \
  3> "$work/display" > "$work/xvfb.log" 2>&1 &
xvfb_pid=$!
if test -n "${TEST_BROWSERS:-}" || test "${TEST_AUDIO:-0}" = 1; then
  python3 "$repo/magnet-linux/tests/browser-page.py" "$work" > "$work/http.log" 2>&1 &
  http_pid=$!
fi
for ((i=0;i<50;i++)); do test ! -s "$work/display" || break; sleep .1; done
DISPLAY=:$(cat "$work/display")
export DISPLAY GDK_BACKEND=x11
port=${SSH_PORT:-22225}
accel=tcg cpu=max
if test -r /dev/kvm && test -w /dev/kvm; then accel=kvm; cpu=host; fi
QEMU_AUDIO=none
if test "${TEST_AUDIO:-0}" = 1; then
  QEMU_AUDIO=wav QEMU_AUDIO_FILE=$work/audio.wav
fi
# shellcheck source=../qemu-audio.sh
source "$repo/magnet-linux/qemu-audio.sh"
qemu-system-x86_64 -name magnet-session-test \
  -machine "q35,accel=$accel" -cpu "$cpu" -m 4096 -smp 4 \
  -drive "if=pflash,format=raw,readonly=on,file=$vm/code.fd" \
  -drive "if=pflash,format=raw,file=$vm/vars.fd" \
  -drive "if=virtio,format=raw,file=$vm/disk.raw" \
  -device virtio-rng-pci -vga none -device virtio-vga-gl,xres=1280,yres=768 \
  -device virtio-keyboard-pci -device virtio-tablet-pci \
  -nic "user,model=virtio-net-pci,hostfwd=tcp:127.0.0.1:$port-:22" \
  -display "${QEMU_DISPLAY:-sdl},gl=on" -monitor none -serial "file:$work/serial.log" "${qemu_audio[@]}" \
  > "$work/qemu.log" 2>&1 &
qemu_pid=$!
options=(-F /dev/null -o BatchMode=yes -o IdentitiesOnly=yes
  -o StrictHostKeyChecking=yes -o HostKeyAlias=magnet-desktop-vm
  -o "UserKnownHostsFile=$vm/known_hosts" -o GlobalKnownHostsFile=/dev/null
  -o ConnectTimeout=2 -i "$vm/client-key" -p "$port")
guest() { ssh -n "${options[@]}" "$user@127.0.0.1" "$@"; }
desktop_guest() {
  # SSH is outside the login's private D-Bus session. Read only its address
  # from our own Waybar process; never source an arbitrary environment file.
  guest 'export XDG_RUNTIME_DIR=/run/user/$(id -u); DBUS_SESSION_BUS_ADDRESS=$(tr "\000" "\n" < /proc/$(pgrep -n -x waybar)/environ | sed -n "s/^DBUS_SESSION_BUS_ADDRESS=//p"); export DBUS_SESSION_BUS_ADDRESS; test -n "$DBUS_SESSION_BUS_ADDRESS"; '"$*"
}
guest_root() {
local command
# Keep the SSH session's shell alive while su changes sessions. If Bash execs
# su as its last command, the PTY can hang up before the target command runs.
printf -v command 'su -c %q; status=$?; exit "$status"' "$1"
python3 - "$vm/credentials.txt" ssh -tt "${options[@]}" "$user@127.0.0.1" "$command" <<'PY'
import os, pathlib, select, subprocess, sys, time
password = next(line[len('Root password: '):] for line in
                pathlib.Path(sys.argv[1]).read_text().splitlines()
                if line.startswith('Root password: '))
process = subprocess.Popen(sys.argv[2:], stdin=subprocess.PIPE,
                           stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
try:
    output = b''
    deadline = time.monotonic() + 30
    while b'Password:' not in output:
        remaining = deadline - time.monotonic()
        if remaining <= 0 or not select.select([process.stdout], [], [], remaining)[0]:
            raise RuntimeError('Timed out waiting for shutdown authentication')
        chunk = os.read(process.stdout.fileno(), 4096)
        if not chunk:
            raise RuntimeError('Shutdown connection closed before authentication')
        output += chunk
    process.stdin.write((password + '\n').encode())
    process.stdin.flush()
    # Keep the PTY input open until the command finishes. communicate() closes
    # stdin immediately, which can hang up Bash before it runs the command.
    result = b''
    deadline = time.monotonic() + 60
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0 or not select.select([process.stdout], [], [], remaining)[0]:
            raise RuntimeError('Timed out waiting for the administrator command')
        chunk = os.read(process.stdout.fileno(), 4096)
        if not chunk:
            break
        result += chunk
    process.wait(timeout=5)
    result = result.decode(errors='replace')
    print(result.replace(password, '[redacted]'))
    if process.returncode:
        raise SystemExit(process.returncode)
finally:
    if process.poll() is None:
        process.kill()
        process.wait()
PY
}
shutdown_guest() {
  local status=0
  guest_root poweroff || status=$?
  # Shutdown can close SSH before the remote shell reports its status.
  # The caller still requires QEMU to exit successfully within the timeout.
  test "$status" -eq 0 || test "$status" -eq 255
}
wait_guest() {
  local command=$1
  for ((attempt=0;attempt<90;attempt++)); do
    kill -0 "$qemu_pid"
    if guest "$command" > /dev/null 2> "$work/ssh.log"; then return; fi
    sleep 1
  done
  scrot "$work/failure.png"
  guest 'cat .local/state/magnet-linux/*.log' > "$work/session-failure.log" || true
  echo "Timed out: $command" >&2
  return 1
}
wait_guest 'pidof gtkgreet >/dev/null'
guest 'set -eu; test "$(readlink /bin/sh)" = bash; test ! -e /bin/oksh; test "$SHELL" = /bin/bash; grep -q "^root:.*:/bin/bash$" /etc/passwd; man -w bash; man -w 5 doas.conf; MANPAGER=cat man bash | sed "s/.\x08//g" | grep SYNOPSIS; MANPAGER=cat man perpd | sed "s/.\x08//g" | grep SYNOPSIS; less --version' > "$work/shell-man.log"
if test "${TEST_DOAS:-0}" = 1; then
  python3 "$repo/magnet-linux/tests/doas.py" "$vm" "$port"
fi
if test "${TEST_TIME:-0}" = 1; then
  wait_guest 'pidof chronyd >/dev/null'
  guest 'chronyc waitsync 60 0 0 1' > "$work/chrony-sync.log"
  guest 'set -eu; chronyc tracking; chronyc -n sources; test "$(ps -o user= -p $(pgrep -x chronyd) | tr -d " ")" = chrony; chronyc tracking | grep -q "Leap status.*Normal"' > "$work/chrony-status.log"
  # The loopback monitoring interface must not accept administrative commands.
  guest 'if chronyc -h 127.0.0.1 makestep; then exit 1; fi' > "$work/chrony-permissions.log"
  old_chronyd=$(guest 'pgrep -x chronyd')
  [[ "$old_chronyd" =~ ^[0-9]+$ ]]
  guest_root "kill -KILL $old_chronyd" > "$work/chrony-restart.log"
  wait_guest "test -n \"\$(pgrep -x chronyd)\" && test \"\$(pgrep -x chronyd)\" != '$old_chronyd'"
  guest 'chronyc waitsync 60 0 0 1' >> "$work/chrony-sync.log"
fi
guest 'test ! -e /bin/sway && test ! -e /bin/swaymsg && test ! -e /bin/start-sway && test ! -e /share/wayland-sessions/magnet-sway.desktop'
guest 'rm -f /tmp/magnet-test-hyprland-ok /tmp/magnet-test-hyprland-relogin-ok /tmp/magnet-test-fuzzel-choice'
sleep 2
window=$(xdotool search --onlyvisible --name 'QEMU.*magnet-session-test' | head -1)
xdotool windowsize --sync "$window" 1280 768
xdotool windowmove --sync "$window" 0 0
xdotool windowfocus --sync "$window"
scrot "$work/01-greeter.png"
type_text() { printf %s "$1" | xdotool type --clearmodifiers --delay 30 --file -; }
username() {
  xdotool key --clearmodifiers ctrl+a
  type_text "$user"
}
password() {
  sleep 1
  sed -n 's/^Password: //p' "$vm/credentials.txt" |
    tr -d '\n' | xdotool type --clearmodifiers --delay 15 --file -
  xdotool key Return
}
# An incorrect password must leave the greeter running without a user session.
username
xdotool key Return
sleep 1
type_text magnet-invalid-test-password
xdotool key Return
sleep 4
guest 'pidof gtkgreet >/dev/null && ! pidof foot >/dev/null && ! pidof waybar >/dev/null && ! XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 monitors >/dev/null 2>&1'
scrot "$work/02-rejected-password.png"
username
xdotool key Return
password
# Authentication goes directly into Hyprland, without choosing a session.
wait_guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j monitors >/dev/null && pidof foot >/dev/null'
wait_guest 'pidof waybar >/dev/null && XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j layers | grep -q waybar'
guest 'cat .local/state/magnet-linux/waybar.log' > "$work/waybar.log"
if grep -Eqi 'module clock:.*disabl|\[error\]' "$work/waybar.log"; then
  cat "$work/waybar.log" >&2
  exit 1
fi
guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j monitors' > "$work/hyprland-monitors.json"
guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j configerrors' > "$work/hyprland-config-errors.txt"
python3 - "$work/hyprland-config-errors.txt" <<'PYTEST'
import json, sys
assert not any(error.strip() for error in json.load(open(sys.argv[1])))
PYTEST
type_text 'test -n "$BASH_VERSION" && printf "MAGNET_HYPRLAND_LOGIN_OK\n" > /tmp/magnet-test-hyprland-ok; id; printf "Hyprland graphical login works\n"'
xdotool key Return
wait_guest 'grep -qx MAGNET_HYPRLAND_LOGIN_OK /tmp/magnet-test-hyprland-ok'
xdotool key --clearmodifiers alt+Return
sleep 2
guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j clients' > "$work/hyprland-clients.json"
python3 - "$work/hyprland-clients.json" <<'PY'
import json, sys
clients = json.load(open(sys.argv[1]))
assert sum(c.get('class') == 'foot' for c in clients) == 2, clients
PY
scrot "$work/03-hyprland.png"
if test "${TEST_NOTIFICATIONS:-0}" = 1; then
  wait_guest 'test "$(pgrep -x mako | wc -l)" -eq 1'
  desktop_guest 'notify-send -t 0 "Magnet notification test" "Notifications are working in Hyprland."'
  for ((i=0;i<30;i++)); do
    if desktop_guest 'makoctl list' > "$work/notifications.log" && grep -q 'Magnet notification test' "$work/notifications.log"; then break; fi
    sleep 1
  done
  grep -q 'Magnet notification test' "$work/notifications.log"
  wait_guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j layers | grep -q notifications'
  sleep 1
  scrot "$work/03-notification.png"
  xdotool mousemove --window "$window" 1100 74 click 3
  sleep 1
  desktop_guest 'makoctl list' > "$work/notifications-clicked.log"
  if grep -q 'Magnet notification test' "$work/notifications-clicked.log"; then exit 1; fi
  desktop_guest 'makoctl dismiss --all; notify-send -t 300 "Magnet expiry test"'
  sleep 1
  desktop_guest 'makoctl list' > "$work/notifications-expired.log"
  if grep -q 'Magnet .*test' "$work/notifications-expired.log"; then exit 1; fi
  old_mako=$(guest 'pgrep -x mako')
  [[ "$old_mako" =~ ^[0-9]+$ ]]
  guest "kill -KILL $old_mako"
  sleep 1
  desktop_guest 'notify-send -u critical "Magnet recovery test" "D-Bus restarted the notification daemon."'
  wait_guest "test -n \"\$(pgrep -x mako)\" && test \"\$(pgrep -x mako)\" != '$old_mako'"
  desktop_guest 'makoctl list' > "$work/notifications-recovery.log"
  grep -q 'Magnet recovery test' "$work/notifications-recovery.log"
  sleep 7
  desktop_guest 'makoctl list' | grep -q 'Magnet recovery test'
  desktop_guest 'makoctl dismiss --all'
fi
if test "${TEST_AUDIO:-0}" = 1; then
  audio_guest() { guest 'export XDG_RUNTIME_DIR=/run/user/$(id -u); '"$*"; }
  wait_guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) wpctl get-volume @DEFAULT_AUDIO_SINK@ >/dev/null'
  audio_guest 'wpctl status; pactl info; aplay -l; perpls -b "$XDG_RUNTIME_DIR/magnet-audio"' > "$work/audio-status.log"
  # Crash recovery must reconnect clients and recreate the default output.
  old_pipewire=$(guest 'pgrep -x pipewire')
  [[ "$old_pipewire" =~ ^[0-9]+$ ]]
  guest "kill -KILL $old_pipewire"
  wait_guest "test \"\$(pgrep -x pipewire)\" != '$old_pipewire' && XDG_RUNTIME_DIR=/run/user/\$(id -u) wpctl get-volume @DEFAULT_AUDIO_SINK@ >/dev/null"
  audio_guest 'wpctl set-volume @DEFAULT_AUDIO_SINK@ 1; wpctl set-mute @DEFAULT_AUDIO_SINK@ 0'
  for pair in pw-play:220 paplay:330 aplay:440; do
    player=${pair%:*} frequency=${pair#*:}
    audio_guest "curl -fsS 'http://10.0.2.2:$(cat "$work/http-port")/tone.wav?frequency=$frequency' -o /tmp/magnet-tone.wav; $player /tmp/magnet-tone.wav"
  done
  xdotool key --clearmodifiers alt+F10
  wait_guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) wpctl get-volume @DEFAULT_AUDIO_SINK@ | grep -q MUTED'
  xdotool key --clearmodifiers alt+F10
  xdotool key --clearmodifiers alt+F11
  wait_guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) wpctl get-volume @DEFAULT_AUDIO_SINK@ | grep -qx "Volume: 0.95"'
  xdotool key --clearmodifiers alt+F12
  wait_guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) wpctl get-volume @DEFAULT_AUDIO_SINK@ | grep -qx "Volume: 1.00"'
  scrot "$work/03-audio-controls.png"
fi
# The bar must reserve screen space, and its workspace buttons must use the
# Lua dispatch API of our Hyprland build. Mouse clicks test the real GTK UI.
guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j layers' > "$work/waybar-layers.json"
python3 - "$work/hyprland-monitors.json" "$work/waybar-layers.json" <<'PY'
import json, sys
monitor = json.load(open(sys.argv[1]))[0]
assert monitor['width'] == 1280 and monitor['height'] == 768, monitor
assert monitor['reserved'][1] >= 32, monitor
layers = json.load(open(sys.argv[2]))
bars = [surface for output in layers.values() for layer in output['levels'].values()
        for surface in layer if surface.get('namespace') == 'waybar']
assert len(bars) == 1, layers
assert bars[0]['y'] == 0 and bars[0]['h'] >= 32, bars
PY
xdotool mousemove --window "$window" 60 16 click 1
wait_guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j activeworkspace | grep -Eq "\"id\": *2,"'
scrot "$work/03-waybar-workspace-2.png"
xdotool key --clearmodifiers alt+1
wait_guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j activeworkspace | grep -Eq "\"id\": *1,"'
xdotool mousemove --window "$window" 1000 600
if test "${TEST_NETWORK:-0}" = 1; then
  guest 'set -eu; nmcli -t -f DEVICE,STATE device | grep -qx eth0:connected; if pidof dhcpcd; then exit 1; fi; ip route; cat /etc/resolv.conf; nmcli general permissions' > "$work/network-status.log"
  guest 'set -eu; nmcli -t -f PERMISSION,VALUE general permissions | grep -qx org.freedesktop.NetworkManager.network-control:yes'
  guest 'set -eu; curl -fIsS --max-time 30 https://example.com >/dev/null'
  # A normal desktop user can save a profile, but an unrelated account cannot
  # change the host network. This must work without a password agent/logind.
  guest 'set -eu; nmcli connection add type ethernet con-name magnet-network-test ifname eth0 connection.autoconnect no ipv4.method manual ipv4.addresses 192.0.2.20/24 ipv4.dns 192.0.2.53 ipv6.method disabled'
  guest_root 'set -eu; test "$(stat -c %a /etc/NetworkManager/system-connections/magnet-network-test.nmconnection)" = 600; nmcli connection reload; if su -s /bin/sh nobody -c "nmcli networking off"; then echo "Unprivileged network change allowed" >&2; exit 1; fi; if id -nG greeter | tr " " "\n" | grep -qx netdev; then exit 1; fi' > "$work/network-policy.log"
  guest 'set -eu; nmcli -g ipv4.addresses connection show magnet-network-test | grep -qx 192.0.2.20/24; nmcli connection modify magnet-network-test ipv4.dns 192.0.2.54'
  # Schedule a reconnect locally before dropping the SSH transport.
  guest 'set -eu; nohup sh -c "sleep 2; nmcli -w 15 device disconnect eth0; sleep 2; nmcli -w 30 device connect eth0" >/tmp/magnet-network-reconnect.log 2>&1 </dev/null &'
  sleep 6
  wait_guest 'nmcli -t -f DEVICE,STATE device | grep -qx eth0:connected'
  guest 'set -eu; cat /tmp/magnet-network-reconnect.log; curl -fIsS --max-time 30 https://example.com >/dev/null' > "$work/network-reconnect.log"
  old_manager=$(guest 'pgrep -x NetworkManager')
  [[ "$old_manager" =~ ^[0-9]+$ ]]
  guest_root "kill -KILL $old_manager" > "$work/network-restart.log"
  wait_guest "test \"\$(pgrep -x NetworkManager)\" != '$old_manager' && nmcli -t -f DEVICE,STATE device | grep -qx eth0:connected"
  # Click the actual bar module and inspect the resulting native Wayland UI.
  xdotool mousemove --window "$window" 970 16 click 1
  wait_guest 'pidof nmtui >/dev/null && XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j clients | grep -q magnet-network'
  sleep 1
  scrot "$work/03-network-nmtui.png"
  xdotool key --clearmodifiers alt+shift+q
  wait_guest '! pidof nmtui >/dev/null'
fi
# Exercise the real launcher binding and its application discovery, then the
# dmenu interface used by scripts. No synthetic application entries are needed.
xdotool key --clearmodifiers alt+d
wait_guest 'pidof fuzzel >/dev/null && XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j layers | grep -q launcher'
type_text foot
sleep 1
scrot "$work/03-fuzzel-foot.png"
xdotool key Return
wait_guest 'test "$(pidof foot | wc -w)" -eq 3 && ! pidof fuzzel >/dev/null'
guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j clients' > "$work/fuzzel-launched-clients.json"
python3 - "$work/fuzzel-launched-clients.json" <<'PY'
import json, sys
clients = json.load(open(sys.argv[1]))
assert sum(c.get('class') == 'foot' for c in clients) == 3, clients
assert all(not c['xwayland'] for c in clients), clients
PY
scrot "$work/03-fuzzel-launched-foot.png"
xdotool key --clearmodifiers alt+shift+q
wait_guest 'test "$(pidof foot | wc -w)" -eq 2'
xdotool key --clearmodifiers alt+d
wait_guest 'pidof fuzzel >/dev/null'
xdotool key Escape
wait_guest '! pidof fuzzel >/dev/null && pidof waybar >/dev/null'
type_text "printf 'alpha\nbeta\n' | fuzzel --dmenu > /tmp/magnet-test-fuzzel-choice"
xdotool key Return
wait_guest 'pidof fuzzel >/dev/null'
type_text beta
sleep 1
scrot "$work/03-fuzzel-dmenu.png"
xdotool key Return
wait_guest 'grep -qx beta /tmp/magnet-test-fuzzel-choice && ! pidof fuzzel >/dev/null'
for browser in ${TEST_BROWSERS:-}; do
  case "$browser" in firefox|chromium) ;; *) echo 'TEST_BROWSERS accepts firefox and chromium.' >&2; exit 2;; esac
  profile=$(guest 'mktemp -d /tmp/magnet-session-browser.XXXXXXXX')
  [[ "$profile" =~ ^/tmp/magnet-session-browser\.[[:alnum:]]+$ ]]
  if test "$browser" = firefox; then
    guest "printf '%s\n' 'user_pref(\"browser.shell.checkDefaultBrowser\", false);' 'user_pref(\"browser.aboutwelcome.enabled\", false);' 'user_pref(\"browser.startup.homepage_override.mstone\", \"ignore\");' > '$profile/user.js'"
    launch="firefox --no-remote --profile '$profile'"
  else
    launch="chromium --user-data-dir='$profile' --no-first-run --no-default-browser-check"
  fi
  type_text "$launch https://example.com > '$profile/browser.log' 2>&1 &"
  xdotool key Return
  wait_guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j clients | grep -q "Example Domain"'
  sleep 1
  scrot "$work/05-$browser-https.png"
  rm -f "$work/javascript-passed"
  xdotool key --clearmodifiers ctrl+l
  sleep .5
  type_text "http://10.0.2.2:$(cat "$work/http-port")/"
  xdotool key Return
  for ((i=0;i<60;i++)); do
    test ! -f "$work/javascript-passed" || break
    sleep 1
  done
  if ! test -f "$work/javascript-passed"; then
    scrot "$work/05-$browser-failed.png"
    guest "cat '$profile/browser.log'" > "$work/$browser.log"
    echo "$browser did not complete the JavaScript page test." >&2
    exit 1
  fi
  sleep 1
  scrot "$work/05-$browser-javascript.png"
  if test "${TEST_AUDIO:-0}" = 1; then
    xdotool key --clearmodifiers ctrl+l
    # Chromium may focus the location entry on the next frame.
    sleep .5
    type_text "http://10.0.2.2:$(cat "$work/http-port")/audio?browser=$browser"
    xdotool key Return
    wait_guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j clients | grep -q "Magnet audio check"'
    xdotool key --clearmodifiers alt+f
    sleep 1
    # Force a motion event after switching fullscreen surfaces. Repeating the
    # previous browser's coordinates can leave the guest pointer focus stale.
    xdotool mousemove --window "$window" 40 80
    sleep .2
    xdotool mousemove --window "$window" 640 384
    sleep .2
    xdotool click 1
    for ((i=0;i<30;i++)); do
      test ! -f "$work/$browser-audio-passed" || break
      sleep 1
    done
    if ! test -f "$work/$browser-audio-passed"; then
      scrot "$work/05-$browser-audio-failed.png"
      guest "cat '$profile/browser.log'" > "$work/$browser.log"
      guest 'cat .local/state/magnet-linux/audio.log' > "$work/audio-failure.log"
      echo "$browser did not complete audio playback." >&2
      exit 1
    fi
    scrot "$work/05-$browser-audio.png"
  fi
  guest "cat '$profile/browser.log'" > "$work/$browser.log"
  if grep -q 'seccomp sandbox violation' "$work/$browser.log"; then
    echo "$browser reported a sandbox violation during playback." >&2
    exit 1
  fi
  guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j clients' > "$work/$browser-clients.json"
  xdotool key --clearmodifiers alt+shift+q
  sleep 2
done
guest 'cat .local/state/magnet-linux/hyprland.log' > "$work/hyprland.log"
xdotool key --clearmodifiers alt+shift+e
wait_guest 'pidof gtkgreet >/dev/null && ! pidof foot >/dev/null && ! pidof waybar >/dev/null && ! XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 monitors >/dev/null 2>&1'
if test "${TEST_AUDIO:-0}" = 1; then
  wait_guest '! pidof pipewire >/dev/null && ! pidof pipewire-pulse >/dev/null && ! pidof wireplumber >/dev/null'
fi
scrot "$work/04-returned-to-greeter.png"
if test "${TEST_NOTIFICATIONS:-0}" = 1; then
  wait_guest '! pidof mako >/dev/null'
fi
# A second login exercises the handoff back from the greeter compositor.
sleep 2
username
xdotool key Return
password
wait_guest 'XDG_RUNTIME_DIR=/run/user/$(id -u) hyprctl -i 0 -j monitors >/dev/null && pidof foot >/dev/null'
wait_guest 'test "$(pidof waybar | wc -w)" -eq 1'
if test "${TEST_NOTIFICATIONS:-0}" = 1; then
  wait_guest 'test "$(pgrep -x mako | wc -l)" -eq 1'
  desktop_guest 'notify-send -t 0 "Magnet relogin test"; makoctl list' > "$work/notifications-relogin.log"
  grep -q 'Magnet relogin test' "$work/notifications-relogin.log"
  desktop_guest 'makoctl dismiss --all'
  echo 'PASS: notification display, dismissal, expiry, critical urgency, D-Bus crash recovery, logout cleanup and re-login'
fi
if test "${TEST_AUDIO:-0}" = 1; then
  wait_guest 'test "$(pgrep -x pipewire | wc -l)" -eq 1 && XDG_RUNTIME_DIR=/run/user/$(id -u) wpctl get-volume @DEFAULT_AUDIO_SINK@ >/dev/null'
fi
type_text 'printf "MAGNET_HYPRLAND_RELOGIN_OK\n" > /tmp/magnet-test-hyprland-relogin-ok'
xdotool key Return
wait_guest 'grep -qx MAGNET_HYPRLAND_RELOGIN_OK /tmp/magnet-test-hyprland-relogin-ok'
scrot "$work/05-hyprland-relogin.png"
if test "${TEST_NETWORK:-0}" = 1; then
  # Reboot proves DHCP, saved profiles and system services survive a real boot.
  old_boot=$(guest 'cat /proc/sys/kernel/random/boot_id')
  guest_root reboot
  sleep 3
  wait_guest "test \"\$(cat /proc/sys/kernel/random/boot_id)\" != '$old_boot' && pidof gtkgreet >/dev/null && nmcli -t -f DEVICE,STATE device | grep -qx eth0:connected"
  guest 'set -eu; nmcli -g ipv4.dns connection show magnet-network-test | grep -qx 192.0.2.54; nmcli connection delete magnet-network-test; curl -fIsS --max-time 30 https://example.com >/dev/null'
  sleep 2
  username
  xdotool key Return
  password
  wait_guest 'pidof waybar >/dev/null && pidof foot >/dev/null'
  scrot "$work/06-network-after-reboot.png"
  echo 'PASS: NetworkManager DHCP/DNS/HTTPS, desktop authorization, denied unprivileged changes, saved profile, reconnect, daemon recovery, nmtui bar control and reboot'
fi
if test "${TEST_TIME:-0}" = 1; then
  old_boot=$(guest 'cat /proc/sys/kernel/random/boot_id')
  guest_root reboot
  sleep 3
  wait_guest "test \"\$(cat /proc/sys/kernel/random/boot_id)\" != '$old_boot' && pidof gtkgreet >/dev/null && pidof chronyd >/dev/null"
  guest 'chronyc waitsync 60 0 0 1' >> "$work/chrony-sync.log"
  guest_root 'test -s /var/lib/chrony/drift; test "$(stat -c %U /var/lib/chrony/drift)" = chrony' > "$work/chrony-drift.log"
  echo 'PASS: chrony NTP synchronization, privilege drop, restricted control, perp recovery, persistent drift and reboot'
fi
shutdown_guest
for ((i=0;i<60;i++)); do
  if ! kill -0 "$qemu_pid" 2>/dev/null; then wait "$qemu_pid"; qemu_pid=; break; fi
  sleep 1
done
test -z "$qemu_pid"
if test "${TEST_AUDIO:-0}" = 1; then
  frequencies=(220 330 440)
  for browser in ${TEST_BROWSERS:-}; do
    case "$browser" in firefox) frequencies+=(660);; chromium) frequencies+=(880);; esac
  done
  python3 "$repo/magnet-linux/tests/audio_fixture.py" "$work/audio.wav" "${frequencies[@]}"
fi
echo 'PASS: Sway absent, graphical login, rejected password, Hyprland, terminal input, Waybar, workspace clicks, Fuzzel launcher and dmenu, logout, re-login, shutdown'

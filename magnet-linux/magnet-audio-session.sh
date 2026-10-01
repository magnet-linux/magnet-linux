#!/bin/sh
# Run desktop audio under a per-user perp supervisor for this login session.
set -eu
test "$#" -gt 0 || { echo 'usage: magnet-audio-session COMMAND [ARG ...]' >&2; exit 2; }
: "${XDG_RUNTIME_DIR:?desktop runtime directory is required}"
state=${XDG_STATE_HOME:-$HOME/.local/state}/magnet-linux
mkdir -p "$state"
umask 077
exec 9> "$XDG_RUNTIME_DIR/magnet-audio.lock"
flock -n 9 || { echo 'An audio session is already running for this account.' >&2; exit 1; }
services=$XDG_RUNTIME_DIR/magnet-audio
mkdir -p "$services"
for service in pipewire pipewire-pulse wireplumber; do
  mkdir -p "$services/$service"
  chmod 1755 "$services/$service"
  ln -sf "/etc/magnet-linux/audio/$service/rc.main" "$services/$service/rc.main"
done
supervisor=''
desktop=''
# Called by the EXIT trap.
# shellcheck disable=SC2329
cleanup() {
  trap - EXIT HUP INT TERM
  if test -n "$desktop"; then
    kill -TERM "$desktop" 2>/dev/null || true
    wait "$desktop" 2>/dev/null || true
  fi
  if test -n "$supervisor"; then
    kill -TERM "$supervisor" 2>/dev/null || true
    wait "$supervisor" 2>/dev/null || true
  fi
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
perpd "$services" >> "$state/audio.log" 2>&1 &
supervisor=$!
# Waybar disables its audio module if the PulseAudio socket is not ready when
# it starts. Wait for an actual connection, not just a socket pathname.
attempt=0
while test "$attempt" -lt 100; do
  attempt=$((attempt + 1))
  if pactl info >/dev/null 2>&1; then break; fi
  kill -0 "$supervisor" || exit 1
  sleep .1
done
if ! pactl info >/dev/null 2>&1; then
  echo "Audio startup failed; see $state/audio.log" >&2
  exit 1
fi
"$@" &
desktop=$!
status=0
wait "$desktop" || status=$?
desktop=
exit "$status"

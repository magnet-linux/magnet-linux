# shellcheck shell=bash
# shellcheck disable=SC2034
# Sourced by VM launchers. Produces the qemu_audio array without opening devices.
# QEMU_AUDIO=wav and QEMU_AUDIO_FILE provide a deterministic playback test sink.
audio_backend=${QEMU_AUDIO:-auto}
audio_drivers=$(qemu-system-x86_64 -audio driver=help 2>&1)
if test "$audio_backend" = auto; then
  audio_backend=none
  if test "${mode:-desktop}" != headless; then
    runtime=${XDG_RUNTIME_DIR:-/run/user/$(id -u)}
    if test -S "$runtime/pipewire-0" && grep -Fxq pipewire <<< "$audio_drivers"; then
      audio_backend=pipewire
    elif test -S "$runtime/pulse/native" && grep -Fxq pa <<< "$audio_drivers"; then
      audio_backend=pa
    fi
  fi
fi
case "$audio_backend" in
  none|pipewire|pa|alsa|sdl) audio_options=$audio_backend,id=magnet-audio;;
  wav)
    : "${QEMU_AUDIO_FILE:?set QEMU_AUDIO_FILE for the wav audio backend}"
    # QEMU uses commas to separate options, and doubles literal commas.
    audio_options="wav,id=magnet-audio,path=${QEMU_AUDIO_FILE//,/,,}";;
  *) echo 'QEMU_AUDIO must be auto, pipewire, pa, alsa, sdl, none or wav.' >&2; exit 2;;
esac
if ! grep -Fxq "$audio_backend" <<< "$audio_drivers"; then
  echo "This QEMU build does not support the $audio_backend audio backend." >&2
  exit 1
fi
audio_device=virtio-sound-pci,audiodev=magnet-audio
# The WAV backend records playback and has no capture implementation.
if test "$audio_backend" = wav; then audio_device+=,streams=1; fi
qemu_audio=(-audiodev "$audio_options" -device "$audio_device")

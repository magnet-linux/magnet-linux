# Desktop audio

`pipewire.jsonnet` packages PipeWire 1.6.9 (including `pipewire-pulse`, the ALSA
plugin and `pw-play`/`pw-record`) and WirePlumber 0.5.18. `libraries.jsonnet`
provides libsndfile, the PulseAudio client libraries/tools and ALSA utilities.
All sources are pinned by SHA-256 and builds run offline. Existing ALSA, GLib,
D-Bus, eudev and Lua libraries are shared with the desktop/browser packages.
Firefox uses libpulse; Chromium uses its existing ALSA backend through the
PipeWire plugin. Both routes share the same mixer and volume controls.

The initial build supports local ALSA playback and capture, native PipeWire
clients, PulseAudio clients and ALSA applications. JACK, Bluetooth and video
capture integrations are not enabled. libsndfile includes its built-in PCM
formats; compressed media decoding remains the application's responsibility.

The graphical session starts `magnet-audio-session` inside its D-Bus session.
A per-user perp instance supervises `pipewire`, `pipewire-pulse` and
`wireplumber`, and shuts them down on logout. The greeter does not start audio.
The desktop account gets the `audio` group; PAM applies the scheduling limits
in `/etc/security/limits.d/10-audio.conf`.

Machine settings remain editable under `/etc/pipewire`, `/etc/wireplumber`
and `/etc/magnet-linux/audio`. Upstream defaults live under `/share`.
WirePlumber remembers device/volume choices in the user's state directory.
Supervisor and daemon output is in `~/.local/state/magnet-linux/audio.log`.
Inspect the session with `wpctl status`, `pactl info`, or
`perpls -b "$XDG_RUNTIME_DIR/magnet-audio"`.

See [the desktop guide](../../magnet-linux/DESKTOP.md) for VM audio routing,
volume controls and the test that checks QEMU's recorded output.

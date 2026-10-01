# Hyprland desktop

`system.jsonnet` is the console/SSH base. `desktop-hyprland.jsonnet` adds a
desktop kernel, Hyprland, Waybar, Fuzzel, Foot, fonts, accelerated graphics and graphical login.
It uses the same Btrfs installation layout and manual snapshot procedure.
Sway is not installed by this profile, including for the login screen.

## Graphical login into Hyprland

`desktop-hyprland.jsonnet` adds Hyprland, greetd, a GTK login screen, and Mesa's
VirGL driver. Its exported selections are `desktop`, `desktop-browser`
(Firefox), `desktop-chromium`, and `desktop-browsers` (both browsers).
Create a VM with an ordinary account and both browsers:

```sh
nix-shell magnet-linux/shell.nix --run \
  'WITH_FIREFOX=1 WITH_CHROMIUM=1 magnet-linux/create-desktop-vm.sh magnet-linux/out/hyprland-vm browser'
./magnet-linux/run-vm.sh magnet-linux/out/hyprland-vm
```

The private `credentials.txt` contains the initial user and root passwords.
The desktop account belongs to `wheel` and can run `doas COMMAND` or `doas -s`
using its own login password. Each invocation asks for authentication.
Administrator rules are manually editable in `/etc/doas.conf`; the greeter
and other service accounts are not enrolled. `setup-login.sh TARGET USER`
enrolls the selected desktop account while preserving existing doas rules.
Log in with your username and password; there is no desktop selector.
Hyprland opens Foot automatically. Alt+Enter opens another terminal,
Alt+D opens Fuzzel (type an application name and press Enter; Escape cancels),
Alt+Shift+E logs out to the login screen,
Alt+Shift+Q closes a window, and Alt+1 through Alt+9 change workspace.
Alt+arrows changes focus; Alt+Shift+arrows swaps the focused tiled window with
its neighbor in that direction.
Waybar starts automatically at the top of the screen, with clickable workspace
buttons 1–9, the focused window title, and a clock. The active workspace is
highlighted; empty workspaces are dimmed.

Mako displays desktop notifications at the upper right. Ordinary notifications
expire after six seconds; critical ones remain until dismissed. Right-click a
notification to dismiss it, or run `makoctl dismiss --all`. Test with
`notify-send 'Hello' 'Notifications are working'`. `makoctl history` shows
dismissed notifications, and `makoctl restore` brings back the most recent one.
The daemon runs as the desktop user and exits with the login session; D-Bus
can restart it when an application next sends a notification.

Click the Ethernet/Wi-Fi label to open NetworkManager's `nmtui` in Foot. The
desktop account can change connections without a root password; `nmcli` is
also installed. NetworkManager handles DHCP and DNS under perp, with
wpa_supplicant for Wi-Fi. The standalone dhcpcd service is disabled in this
profile. Configuration and saved profiles live under `/etc/NetworkManager`.
The QEMU Ethernet adapter is tested; physical Wi-Fi needs its hardware driver,
firmware and regulatory data.

PipeWire and WirePlumber start with the user session. Waybar displays the
output volume: scroll to change it and click to mute. Media keys control volume
and mute; Alt+F10 mutes, Alt+F11 lowers volume and Alt+F12 raises it. The initial
profile caps these controls at 100%. `wpctl status` lists outputs and inputs;
`wpctl set-default ID` selects a device. `pw-play FILE.wav`, `paplay FILE.wav`
and `aplay FILE.wav` exercise native PipeWire, PulseAudio and ALSA clients.

The launcher exposes a stereo VirtIO sound device for playback and capture.
For graphical launches, `QEMU_AUDIO=auto` selects the host's PipeWire or
PulseAudio socket when available. `QEMU_AUDIO=none` disables host sound;
`QEMU_AUDIO=pipewire`, `pa`, `alsa` or `sdl` selects a backend explicitly.
Headless launches default to `none`. For deterministic output capture, use
`QEMU_AUDIO=wav QEMU_AUDIO_FILE=/absolute/path/output.wav`; this does not use
the host speakers or microphone.

The VM records `virgl` in its `graphics` file, which tells the launcher to
enable QEMU's accelerated VirtIO GPU and SDL OpenGL display. SDL avoids the
black GTK OpenGL window observed with NVIDIA on X11. Set `QEMU_DISPLAY=gtk`
to select GTK explicitly. This requires an OpenGL-capable host display.
`--headless` uses a serial console and an unaccelerated display, for console
and SSH work only. Ctrl+Alt+F2 opens the spare text login
in a graphical VM; Ctrl+Alt+F1 returns to the graphical session.

Machine defaults are manually editable:

- `/etc/greetd/config.toml`: login daemon and greeter configuration.
- `/etc/greetd/hyprland.lua` and `style.css`: login screen layout and appearance.
  The greeter runs in a dedicated Hyprland session with no desktop shortcuts.
- `/etc/pam.d/greetd`: authentication against local accounts and `/etc/shadow`.
- `/etc/xdg/hypr/hyprland.lua`: default Hyprland configuration. A user's
  `~/.config/hypr/hyprland.lua` takes precedence.
- `/etc/xdg/waybar/config.jsonc` and `style.css`: bar modules and appearance.
  User overrides belong in `~/.config/waybar/`.
- `/etc/xdg/fuzzel/fuzzel.ini`: launcher font, colors and terminal command.
  User overrides belong in `~/.config/fuzzel/fuzzel.ini`.
- `/etc/xdg/mako/config`: notification appearance, placement and timeouts.
  User overrides belong in `~/.config/mako/config`; run `makoctl reload`
  after editing the active file. To switch from the system file to a newly
  created user override, restart Mako or log out and back in.
- `/etc/chrony.conf`: automatic time synchronization, supervised by perp.
  `chronyc tracking` and `chronyc -n sources` show synchronization status.
- `/etc/localtime`: the system timezone, initially a symlink to
  `/share/zoneinfo/Etc/UTC`. The IANA timezone database is installed; for example,
  root can select New Zealand time with
  `ln -sf /share/zoneinfo/Pacific/Auckland /etc/localtime`, then restart the session.
- `/etc/magnet-linux/audio/*/rc.main`: per-user perp audio services.
- `/etc/asound.conf`: route ALSA applications through PipeWire.
- `/etc/wireplumber/wireplumber.conf.d/10-magnet.conf`: local sound-device policy.
  PipeWire overrides may be added under `/etc/pipewire/pipewire.conf.d/`.
- `/etc/security/limits.d/10-audio.conf`: scheduling limits applied by greetd's
  PAM session to desktop accounts in the `audio` group.

`setup-login.sh TARGET USER` provisions these files only when absent, creates
a locked greeter account, and replaces the initial tty1 getty with perp's
`greetd` service. Perp also supervises `udevd`, `seatd`, and the tty2 getty. Serial login
and SSH remain available. Run it after installing a Hyprland selection and
creating the user. Session logs are in `~/.local/state/magnet-linux/`;
the root-only daemon log is `/var/log/greetd.log`.
Waybar's startup and module diagnostics are in
`~/.local/state/magnet-linux/waybar.log`.
Notification diagnostics are in `~/.local/state/magnet-linux/notifications.log`.

The desktop and graphical login use Adwaita cursors at size 24. The session
launcher defaults `XCURSOR_THEME` to `Adwaita` and `XCURSOR_SIZE` to `24`, while
respecting existing values. Applications without an explicit theme inherit
Adwaita through `/share/icons/default/index.theme`.

The initial Hyprland session uses native Wayland applications and basic tiling.
Animations, blur and shadows are enabled, with
a modest blur size of 3 and one pass. Blur is visible behind transparent
surfaces; opaque applications remain opaque. These effects can be disabled
individually in the Hyprland configuration. It does not yet include Xwayland,
screen sharing, a screen locker, Bluetooth audio or Hyprland's optional GUI
dialog helpers. The graphical login screen is not a screen locker.

Test real graphical authentication, logout and a second Hyprland login with:

```sh
nix-shell magnet-linux/shell.nix --run \
  'magnet-linux/tests/session-boot.sh magnet-linux/out/hyprland-vm'
```

The test uses a private Xvfb/QEMU display, scrot and xdotool, and retains
screenshots and logs under `out/session-test.*`. Shut down the VM before
running it. Set `TEST_BROWSERS="firefox chromium"` to also check HTTPS and
JavaScript pages under Hyprland when both browsers are installed. Changes made
by the test persist in the image.
It also checks that Waybar reserves space above application windows, switches
workspace when clicked, exits on logout, and starts once after logging in again.
It exercises Alt+D by launching Foot from Fuzzel, checks Escape dismisses the
launcher, and selects a piped choice using `fuzzel --dmenu`.

Set `TEST_NETWORK=1` to check DHCP, DNS and HTTPS, saved profile permissions,
desktop-user authorization, denied changes from an unrelated user, Ethernet
reconnection, daemon recovery and the Waybar network button. It also reboots
the VM and verifies the saved profile and connectivity survive, then removes
the temporary profile. This can be combined with the audio/browser tests.

Set `TEST_TIME=1 TEST_NOTIFICATIONS=1` to verify NTP synchronization, chronyd's
privilege drop and restart/reboot behavior, notification rendering and expiry,
critical notifications, D-Bus recovery, and cleanup across two logins. The
time test needs outbound NTP access to the configured sources.

Set `TEST_DOAS=1` to check authentication with the desktop user's password,
wrong-password rejection, root identity, environment reset, command exit
status, file permissions, and denial for non-administrators. The same checks
can run against an already booted VM with
`python3 magnet-linux/tests/doas.py VM-DIRECTORY SSH-PORT`.

Set `TEST_AUDIO=1` to capture QEMU's output and check known tones from native
PipeWire, PulseAudio and ALSA clients. With `TEST_BROWSERS="firefox chromium"`,
it also plays a WAV file in each browser and checks its distinct tone in the
recording. The test exercises service crash recovery, mute/volume shortcuts,
audio shutdown at logout and startup on a second login:

```sh
TEST_AUDIO=1 TEST_BROWSERS="firefox chromium" nix-shell magnet-linux/shell.nix \
  --run 'magnet-linux/tests/session-boot.sh magnet-linux/out/perp-vm'
```

For an existing Hyprland installation, sync the updated profile into an inactive
root and run `setup-login.sh` there to provision the bar defaults. Add
`hl.exec_cmd("waybar")` to your existing `hl.on("hyprland.start", ...)` callback
in the active Hyprland configuration; provisioning preserves existing files.
For the launcher, add `hl.bind("ALT + D", hl.dsp.exec_cmd("fuzzel"))` to that
configuration. The updated profile includes Fuzzel, and `setup-login.sh`
provisions its configuration when absent. Fuzzel searches the installed
application desktop files, including Foot and whichever browsers you selected.
For scripts, `printf 'one\ntwo\n' | fuzzel --dmenu` prints the chosen line to
standard output.

To add audio to an existing installation, also install the updated desktop
kernel and restart QEMU with the updated `run-vm.sh` so it exposes the sound
device. `setup-login.sh` adds the account to `audio` and provisions missing
ALSA, WirePlumber, perp and scheduling-limit settings. Since it preserves
existing files, merge `session required pam_limits.so` into `/etc/pam.d/greetd`
and the volume bindings and Waybar audio module from `graphical-skeleton` into
your existing Hyprland and bar configurations. Log out and back in to apply the
group membership and PAM limits.

When updating an older Hyprland installation, package sync removes Sway and
its session launcher. As usual, it preserves `/etc`: explicitly update
`/etc/greetd/config.toml` from the graphical skeleton to launch
`/bin/magnet-session --greeter`, and copy the new `/etc/greetd/hyprland.lua`.
The old `/etc/greetd/sway.conf`, `/etc/greetd/environments` and `/etc/sway/config`
can be removed after saving any local edits. The fixed-command greeter ignores
an old environments file. Update the installed recipe tree as well.

## Optional browsers and persistent VMs

`create-desktop-vm.sh [NEW-VM-DIRECTORY [USERNAME]]` installs Hyprland with
its graphical login screen. The account defaults to `browser`; root desktop
sessions are not supported. Set `WITH_FIREFOX=1`, `WITH_CHROMIUM=1`, or both
to include browsers. The corresponding roots in `desktop-hyprland.jsonnet`
are `desktop-browser`, `desktop-chromium`, and `desktop-browsers`; `desktop`
contains no browser. The former `WITH_HYPRLAND=1` opt-in is accepted but no
longer necessary.

Creation builds missing packages, creates a sparse 24 GiB disk, and uses sudo
to mount only that new image. Existing destinations are refused. The VM
includes the editable package tree, bootstrap seed, NetworkManager, OpenSSH,
and a matching UEFI firmware pair. Its private `credentials.txt` contains
random initial user and root passwords. SSH uses a private key and pinned
host key; password authentication remains disabled.

```sh
nix-shell magnet-linux/shell.nix --run \
  'magnet-linux/create-desktop-vm.sh magnet-linux/out/desktop-vm'
./magnet-linux/run-vm.sh magnet-linux/out/desktop-vm
```

Log in graphically, use Alt+D to launch applications, and shut down with
`doas poweroff`. Changes persist across boots. `run-vm.sh --ssh VM-DIRECTORY`
connects over SSH; `run-vm.sh --headless VM-DIRECTORY` boots into a serial
console. Defaults are 4 GiB RAM, four CPUs, and SSH on localhost port 22222.
Override `QEMU_MEMORY`, `QEMU_CPUS`, or `SSH_PORT` as needed. A lock prevents
launching the same disk twice.

[Firefox](https://github.com/magnet-linux/packages/blob/main/firefox/README.md) and
[Chromium](https://github.com/magnet-linux/packages/blob/main/chromium/README.md) install globally and share libraries
from [browser-common](https://github.com/magnet-linux/packages/blob/main/browser-common/README.md). Both currently
use software rendering. Firefox uses PipeWire's PulseAudio compatibility
server; Chromium uses its ALSA plugin. WebRTC is disabled in Firefox; GPU
acceleration and CUPS printing are disabled in Chromium.

## Installation onto a mounted filesystem

Follow [README.md](README.md), selecting the desktop in both build and sync:

```sh
magnet-linux/_work/magpkg build --store "$store" --jobs 3 --parallelism 8 \
  magnet-linux/desktop-hyprland.jsonnet desktop
sudo magnet-linux/_work/magpkg sync --store "$store" --no-build \
  --no-binary-cache --dest "$target" magnet-linux/desktop-hyprland.jsonnet desktop
```

Run `setup-root.sh` and `setup-tree.sh` as described in the base instructions.
Mount `/home` and `/var`, create an ordinary account, and provision graphical
login:

```sh
sudo chroot "$target" /bin/useradd -m -g users -s /bin/bash alice
sudo chroot "$target" /bin/passwd alice
sudo magnet-linux/setup-login.sh "$target" alice
```

`setup-login.sh` also configures desktop devices, network and administrator
access. It changes an untouched base package selection to the Hyprland
`desktop` root, preserving custom selections. If you selected a browser root,
record that root in `/etc/magnet-linux/system.jsonnet` as well. Finish with
`setup-boot.sh` and unmount the installation before booting it.

## Configuration and updates

The initializers copy missing defaults without replacing administrator edits.
In addition to the graphical settings above, `/etc/xdg/foot/foot.ini` configures
the terminal, `/etc/fonts` controls font discovery, and
`/etc/magnet-linux/desktop-users` lists accounts whose runtime directories
are created at boot. User overrides belong under `~/.config/`.

System configuration rolls back with `/etc`; user configuration in `/home`
persists. Desktop users join `seat`, `video`, `render`, and `audio` for local
device access. The default QEMU output is 1280×768; edit the monitor rule in
`/etc/xdg/hypr/hyprland.lua` to change it.

Package recipes and skeletons are included in the installed tree.
`setup-tree.sh` preserves an existing tree, so updating its recipes is an
explicit administrator operation. Build and sync into an inactive root as
described in the base README. The selection includes the desktop kernel.

Libraries are built for musl and shared globally. Compiler, Meson, Ninja,
CMake, and Python build dependencies are not automatically installed as
runtime tools. Sources are pinned by SHA-256. The base uses GNU tools,
util-linux, shadow, and Bash. Read installed manuals with `man bash`,
`man ls`, or `man doas`; press `q` to leave the pager.

## Fresh installation test

```sh
nix-shell magnet-linux/shell.nix --run magnet-linux/tests/desktop-boot.sh
```

This creates a new VM through the public installer, then runs the graphical
session test described above. Artifacts and the stopped VM are retained under
`out/desktop-test.*`. Set `WITH_FIREFOX=1 WITH_CHROMIUM=1` and
`TEST_BROWSERS="firefox chromium"` to include both browser tests.

The old Sway recipes, profiles, launcher, and tests have been removed from this
checkout. Existing VM images retain their own installed trees and continue to
boot; migrate them explicitly to a Hyprland selection to update their desktop.

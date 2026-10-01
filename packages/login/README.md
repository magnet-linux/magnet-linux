# Graphical login

`login.jsonnet` builds greetd 0.10.3, gtkgreet 0.8 and Linux-PAM 1.7.3 for musl.
Greetd uses the existing source-built Rust toolchain and pinned, offline Cargo
dependencies. PAM authenticates local accounts with `pam_unix`.

The greeter is an ordinary fullscreen GTK Wayland client inside a dedicated
Hyprland session. `fixed-command.patch` makes gtkgreet's `--command` argument
select a fixed session and omit the editable session selector. The greeter
always starts `magnet-session hyprland`; `/etc/greetd/environments` is not used
in this mode. Authentication still happens in greetd through PAM.

`session.jsonnet` installs `magnet-session hyprland`, the greetd launcher,
and the Hyprland Wayland session entry. Sessions get a private D-Bus session
and use the runtime directory provisioned by the distro's desktop service.
`magnet-session --greeter` uses `/etc/greetd/hyprland.lua`, with no desktop key
bindings or terminal launcher. The greeter compositor exits before the user
compositor takes the seat. Sway is not part of this package's runtime closure.

Configuration files are installed once by
[`setup-login.sh`](../../magnet-linux/setup-login.sh), so package syncs do not
replace manually edited `/etc` files. The greeter account is locked and has a
separate PAM service. This is a login manager; it does not provide session
locking. See [DESKTOP.md](../../magnet-linux/DESKTOP.md) for setup and testing.

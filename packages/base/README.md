# Base userland and init

`../base.jsonnet` selects the installed userland. `tools.jsonnet` packages
util-linux, shadow, procps-ng, iproute2, dhcpcd, nano and the selected
inetutils clients. GNU file/text/archive utilities come from `../core.jsonnet`.
The build bootstrap may still contain BusyBox; these packages do not install
it in the runtime root.

Shadow owns login, su, passwd and account management. Util-linux supplies
agetty, mount, disk and namespace tools; its overlapping account commands
are disabled. Procps owns kill and process tools. The existing libuuid recipe
is shared through `../util-linux.jsonnet`, preserving desktop package identity.
Bash from `../core.jsonnet` supplies `/bin/bash` and the `/bin/sh` symlink.
Login accounts use `/bin/bash` so interactive sessions get normal Bash behavior.
`manuals.jsonnet` provides mandoc (`man`, `apropos`, `whatis`, `makewhatis`)
and the less pager; it searches `/share/man` by default. Packages retain
their own manuals. Run `makewhatis /share/man` as root after package changes
when you want an updated keyword index for `apropos` and `man -k`.
`../doas.jsonnet` adds OpenDoas with PAM authentication for administrator
commands. The distro initializes `/etc/doas.conf` and `/etc/pam.d/doas` and
explicitly enrolls administrator accounts in `wheel`.

`init.jsonnet` builds sinit 1.1 and perp 2.07. The clock and umask patches,
and the final shutdown helper, come from [Oasis](https://github.com/oasislinux/oasis/tree/master/pkg).
Their license is retained in `oasis-LICENSE`; perp patches use the terms of
the files they modify. The shutdown helper is installed in `/libexec` and is
called by `/etc/rc.shutdown`, not as a setuid program. Public `poweroff` and
`reboot` commands signal sinit. No init behavior is embedded in Jsonnet.

The shadow patch corrects the disabled-nscd stub's parameter type in 4.20.3.
The iproute2 patch includes libc IPv6 definitions before Linux UAPI headers
on musl.

Machine configuration is provisioned separately from
`magnet-linux/{skeleton,desktop-skeleton,graphical-skeleton}`. Package installs
do not own `/etc` files or enable services. See the distro README for service
management and the installation and graphical boot tests.

# Desktop networking

`network.jsonnet` builds NetworkManager with nmcli, nmtui, its internal DHCP
client, and the wpa_supplicant Wi-Fi backend. It reuses the existing GLib,
D-Bus, udev, NSS, curl, readline, PAM and libuuid packages. The new supporting
libraries are libnl, libndp, S-Lang, newt, Duktape and gettext.

NetworkManager, its persistent dispatcher, the system D-Bus daemon, polkitd
and wpa_supplicant run under perp. There is no systemd, elogind or ConsoleKit
service. Polkit's ConsoleKit
backend permits operation without session tracking; an explicit `netdev`
group rule grants selected networking actions. Membership grants control of
machine-wide connections even over SSH. The greeter is not a member. This
package omits pkexec and the setuid authentication helper; interactive polkit
password prompts are not configured.

`magnet-linux/setup-network.sh TARGET USER` provisions accounts, configuration
and services for an offline target, retaining existing configuration files.
The graphical setup calls it automatically and disables the stock standalone
dhcpcd service. Do not run two DHCP managers on the same interface.
The skeleton includes a persistent DHCP/SLAAC wired profile, shared by Ethernet
adapters. It stays available when additional, non-autoconnecting profiles are
saved; NetworkManager's transient automatic wired profile would not.

The libndp patch adds the socket-address cast required by musl and GCC 15.
S-Lang explicitly links ncursesw for its termcap calls. The polkit package
removes hardcoded `/usr/lib` systemd assets to retain the root-prefix layout.
Wi-Fi userspace includes WPA2/WPA3 and common EAP methods. Physical machines
still need the appropriate kernel driver, firmware and regulatory database;
the initial VM kernel has virtio Ethernet and the generic wireless/rfkill
interfaces, not a selection of hardware Wi-Fi drivers.

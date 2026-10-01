local b = import './build.libsonnet';
local d = import './libraries.jsonnet';
local l = import '../desktop-libs.jsonnet';
local common = import '../browser-common/libraries.jsonnet';
local polkit = b.meson('polkit',
  '-Dsession_tracking=ConsoleKit -Dintrospection=false -Dtests=false -Dexamples=false -Dman=false -Dgtk_doc=false -Dgettext=false -Dpam_prefix=/share/pam -Dos_type=lfs',
  [l.glib, l.expat, d.duktape, common.dbus, (import '../login/login.jsonnet').pam],
  [(import '../kernel-tools.jsonnet').perl, d.gettext], '', |||
    # Networking uses explicit group rules, provisioned separately. Keep the
    # package free of editable /etc files and unnecessary setuid executables.
    rm -rf /out/etc /out/var /out/usr
    rm -f /out/bin/pkexec /out/lib/polkit-1/polkit-agent-helper-1
    install -Dm644 COPYING /out/share/licenses/polkit/COPYING
  |||);
local supplicant = b.package('wpa_supplicant', |||
  cd wpa_supplicant
  cat > .config <<'MAGNET_CONFIG'
||| + (importstr './wpa.config') + |||
  MAGNET_CONFIG
  make -j"$BUILD_PARALLELISM" BINDIR=/bin LIBDIR=/lib
  make DESTDIR=/out BINDIR=/bin LIBDIR=/lib install
  install -Dm644 dbus/dbus-wpa_supplicant.conf /out/share/dbus-1/system.d/wpa_supplicant.conf
  # perp supervises the daemon; D-Bus must not start a second instance.
  install -Dm644 ../COPYING /out/share/licenses/wpa_supplicant/COPYING
|||, [d.libnl, common.dbus, (import '../openssl.jsonnet').openssl]);
local manager = b.meson('NetworkManager',
  '-Dsbindir=bin -Dsystemdsystemunitdir=no -Dsystemdsystemgeneratordir=no ' +
  '-Dsession_tracking=no -Dsession_tracking_consolekit=false -Dsuspend_resume=consolekit ' +
  '-Dsystemd_journal=false -Dselinux=false -Dlibaudit=no -Dpolkit=true ' +
  '-Dconfig_auth_polkit_default=true -Dconfig_dns_rc_manager_default=file ' +
  '-Dconfig_dhcp_default=internal -Dconfig_wifi_backend_default=wpa_supplicant ' +
  '-Dwifi=true -Diwd=false -Dppp=false -Dmodem_manager=false -Dovs=false ' +
  '-Dnm_cloud_setup=false -Dnbft=false -Dclat=false -Debpf=false -Dlibpsl=false ' +
  '-Dfirewalld_zone=false -Difupdown=false -Dcrypto=nss -Dqt=false ' +
  '-Dnmcli=true -Dnmtui=true -Dreadline=libreadline -Dintrospection=false ' +
  '-Dvapi=false -Ddocs=false -Dman=false -Dtests=no -Druntime_dir=/run ' +
  '-Dudev_dir=/lib/udev -Ddbus_conf_dir=/share/dbus-1/system.d',
  [l.glib, l.eudev, common.dbus, common.curl, d.libndp, d.newt, d.gettext,
   (import '../util-linux.jsonnet').uuid, (import '../hyprland/dependencies.jsonnet').readline,
   (import '../chromium/dependencies.jsonnet').nss, polkit, supplicant],
  [(import '../kernel-tools.jsonnet').perl, (import '../core.jsonnet').bash], '', |||
    # Keep generated examples, but let setup-network.sh own initial /etc state.
    rm -rf /out/etc /out/var
    # The manager and persistent dispatcher are supervised by perp.
    rm -f /out/share/dbus-1/system-services/org.freedesktop.NetworkManager.service
    rm -f /out/share/dbus-1/system-services/org.freedesktop.nm_dispatcher.service
    # Our eudev has no kmod builtin and the VM kernel uses built-in drivers.
    rm -f /out/lib/udev/rules.d/90-nm-thunderbolt.rules
    install -Dm644 COPYING /out/share/licenses/NetworkManager/COPYING
    /out/bin/NetworkManager --version
  |||);
{ networkmanager: manager, polkit: polkit, wpa_supplicant: supplicant }

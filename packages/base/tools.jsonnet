local b = import './build.libsonnet';
local core = import '../core.jsonnet';
local ncurses = (import '../desktop-libs.jsonnet').ncurses;
local kernelTools = import '../kernel-tools.jsonnet';
local shadow = b.autotools('shadow',
  '--without-libpam --without-audit --without-selinux --without-acl --without-btrfs --without-libbsd --without-nscd --without-sssd --disable-logind --disable-nls --disable-man --disable-subordinate-ids',
  [], [], "patch -p1 <<'PATCH'\n" + (importstr './shadow-nscd.patch') + "\nPATCH\n", |||
    # Configuration and accounts belong to the installation, not the archive.
    mkdir -p /out/share/shadow
    cp etc/login.defs /out/share/shadow/login.defs.example
    rm -rf /out/etc
    # Upstream uses separate ubindir/usbindir variables in addition to sbindir.
    if test -d /out/sbin; then cp -a /out/sbin/. /out/bin/; rm -rf /out/sbin; fi
    chmod 4755 /out/bin/su /out/bin/passwd
    install -Dm644 COPYING /out/share/licenses/shadow/COPYING
  |||);
local util = b.autotools('util-linux',
  '--disable-makeinstall-chown --disable-use-tty-group --disable-nls --disable-asciidoc --disable-poman --disable-login --disable-su --disable-runuser --disable-chfn-chsh --disable-newgrp --disable-nologin --disable-vipw --disable-kill --disable-liblastlog2 --disable-pam-lastlog2 --without-python --without-systemd --without-systemdsystemunitdir --without-udev --without-selinux --without-audit',
  [(import '../util-linux.jsonnet').uuid, ncurses], [core.gawk, core.bash], '', |||
    rm -rf /out/etc /out/var
    # Reuse the existing ABI-identical libuuid package used by Hyprland.
    rm -f /out/lib/libuuid* /out/lib/pkgconfig/uuid.pc
    rm -rf /out/include/uuid
    rm -f /out/share/man/man3/uuid*.3 /out/share/man/man5/terminal-colors.d.5
    # Upstream's usrsbin_execdir is separate from --sbindir. Normalize it
    # and preserve root-layout's /sbin compatibility link, including when
    # the older libuuid archive contributes an empty sbin directory.
    if test -d /out/sbin; then cp -a /out/sbin/. /out/bin/; rm -rf /out/sbin; fi
    ln -s bin /out/sbin
    install -Dm644 README.licensing /out/share/licenses/util-linux/README.licensing
  |||);
local procps = b.autotools('procps-ng',
  '--disable-nls --disable-modern-top --without-systemd --with-ncurses --enable-watch8bit',
  [ncurses], [], '', |||
    install -Dm644 COPYING /out/share/licenses/procps-ng/COPYING
    install -Dm644 COPYING.LIB /out/share/licenses/procps-ng/COPYING.LIB
  |||);
local iproute = b.package('iproute2',
  "patch -p1 <<'PATCH'\n" + (importstr './iproute2-musl.patch') + "\nPATCH\n" + |||
  ./configure
  make -j"$BUILD_PARALLELISM" PREFIX=/ SBINDIR=/bin LIBDIR=/lib
  make DESTDIR=/out PREFIX=/ SBINDIR=/bin LIBDIR=/lib install
  rm -rf /out/etc
  install -Dm644 COPYING /out/share/licenses/iproute2/COPYING
|||, [], [kernelTools.bison, kernelTools.flex, kernelTools.m4, (import '../libelf.jsonnet').libelf]);
local dhcpcd = b.package('dhcpcd', |||
  ./configure --prefix=/ --sbindir=/bin --libexecdir=/libexec \
    --sysconfdir=/etc --localstatedir=/var --rundir=/run --privsepuser=dhcpcd
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
  mkdir -p /out/share/dhcpcd
  mv /out/etc/dhcpcd.conf /out/share/dhcpcd/dhcpcd.conf.example
  rm -rf /out/etc /out/var /out/run
  install -Dm644 LICENSE /out/share/licenses/dhcpcd/LICENSE
|||);
local nano = b.autotools('nano', '--disable-nls --enable-utf8 --with-slang=no',
  [ncurses], [], 'export CPPFLAGS=-I/include/ncursesw\n', |||
    install -Dm644 COPYING /out/share/licenses/nano/COPYING
  |||);
local inetutils = b.autotools('inetutils',
  '--disable-servers --disable-clients --enable-hostname --enable-dnsdomainname --enable-ping --enable-ping6 --enable-traceroute --without-pam --without-wrap',
  [], [], '', |||
    chmod 4755 /out/bin/ping /out/bin/ping6
    install -Dm644 COPYING /out/share/licenses/inetutils/COPYING
  |||);
{
  shadow: shadow, util_linux: util, procps: procps,
  iproute2: iproute, dhcpcd: dhcpcd,
  nano: nano, inetutils: inetutils,
  // Bash supplies both the interactive shell and the POSIX /bin/sh entry point.
  bash: core.bash,
  // Enough for installed shell wrappers, without dragging in init/networking.
  shell_tools: {
    name: 'shell-tools', build: { kind: 'none' }, buildDeps: [], fetch: [],
    runDeps: [core.bash, core.coreutils, core.grep, core.sed, core.gawk, core.findutils],
  },
}

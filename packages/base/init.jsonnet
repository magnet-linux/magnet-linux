local b = import './build.libsonnet';
local tools = import './tools.jsonnet';
local perp = b.package('perp',
  "patch -p1 <<'PATCH'\n" + (importstr './0001-tain_now-Use-clock_gettime.patch') + "\nPATCH\n" +
  "patch -p1 <<'PATCH'\n" + (importstr './0002-perpd-Restore-umask-after-perpd_control_init.patch') + "\nPATCH\n" + |||
    # Keep each submake's include flags while selecting the C dialect.
    sed -i 's/^CFLAGS =.*/CFLAGS = -O2 -std=gnu17 -DNDEBUG -D_GNU_SOURCE/' conf.mk
    make -j"$BUILD_PARALLELISM"
    make BINDIR=/out/bin SBINDIR=/out/bin MANDIR=/out/share/man install
    install -Dm644 LICENSE /out/share/licenses/perp/LICENSE
  |||, [tools.bash, (import '../core.jsonnet').gzip]);
local sinit = b.package('sinit', |||
  sed 's|/bin/rc\.|/etc/rc.|g' config.def.h > config.h
  make CC=gcc CFLAGS='-O2 -std=gnu17 -D_DEFAULT_SOURCE' LDFLAGS=
  install -Dm755 sinit /out/bin/sinit
  ln -s sinit /out/bin/init
  install -Dm644 sinit.8 /out/share/man/man8/sinit.8
  install -Dm644 LICENSE /out/share/licenses/sinit/LICENSE
  cat > shutdown.c <<'SOURCE'
||| + (importstr './shutdown.c') + |||
  SOURCE
  gcc -O2 -std=gnu17 -o shutdown shutdown.c
  install -Dm755 shutdown /out/libexec/magnet-shutdown
  cat > /out/share/licenses/sinit/oasis-LICENSE <<'LICENSE'
||| + (importstr './oasis-LICENSE') + |||
  LICENSE
  cat > /out/bin/poweroff <<'SCRIPT'
  #!/bin/sh
  test "$(id -u)" = 0 || { echo 'poweroff requires root' >&2; exit 1; }
  test "$#" = 0 || { echo 'usage: poweroff' >&2; exit 2; }
  kill -USR1 1
  SCRIPT
  cat > /out/bin/reboot <<'SCRIPT'
  #!/bin/sh
  test "$(id -u)" = 0 || { echo 'reboot requires root' >&2; exit 1; }
  test "$#" = 0 || { echo 'usage: reboot' >&2; exit 2; }
  kill -INT 1
  SCRIPT
  chmod 0755 /out/bin/poweroff /out/bin/reboot
|||, [tools.shell_tools, perp]);
{ perp: perp, sinit: sinit }

local b = import './build.libsonnet';
local ncurses = (import '../desktop-libs.jsonnet').ncurses;
local less = b.autotools('less', '--with-regex=posix', [ncurses], [],
  'export CPPFLAGS=-I/include/ncursesw\n', |||
    install -Dm644 LICENSE /out/share/licenses/less/LICENSE
  |||);
local mandoc = b.package('mandoc', |||
  cat > configure.local <<'EOF'
  PREFIX=/
  BINDIR=/bin
  SBINDIR=/bin
  MANDIR=/share/man
  BIN_FROM_SBIN=.
  MANPATH_DEFAULT=/share/man:/local/share/man:/local/man
  MANPATH_BASE=/share/man
  BINM_PAGER=less
  LN='ln -sf'
  EOF
  ./configure
  make -j"$BUILD_PARALLELISM"
  make DESTDIR=/out install
  install -Dm644 LICENSE /out/share/licenses/mandoc/LICENSE
|||, [(import '../zlib.jsonnet').zlib, less]);
{ less: less, mandoc: mandoc }

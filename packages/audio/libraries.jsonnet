local b = import './build.libsonnet';
local l = import '../desktop-libs.jsonnet';
local t = import '../desktop-tools.jsonnet';
local common = import '../browser-common/libraries.jsonnet';
local sndfile = b.autotools('libsndfile',
  '--disable-external-libs --disable-mpeg --disable-sqlite --disable-full-suite',
  [], [t.python], '', |||
    install -Dm644 COPYING /out/share/licenses/libsndfile/COPYING
  |||);
// PulseAudio client libraries and tools, served by pipewire-pulse.
local pulse = b.meson('pulseaudio',
  '-Dauto_features=disabled -Ddaemon=false -Dclient=true -Dglib=enabled -Ddatabase=simple -Dtests=false -Dman=false -Ddoxygen=false',
  [sndfile, l.glib], [(import '../kernel-tools.jsonnet').m4], '', |||
    # Keep local configuration separate from package-owned files.
    rm -rf /out/etc
    install -Dm644 LICENSE /out/share/licenses/libpulse/LICENSE
  |||) { name: 'libpulse-17.0' };
local utils = b.autotools('alsa-utils',
  '--sbindir=/bin --with-curses=ncursesw --disable-nls --disable-alsaconf --disable-alsaloop --disable-bat --disable-xmlto --disable-rst2man --with-systemdsystemunitdir=no',
  [common.alsa, l.ncurses], [], '', |||
    rm -rf /out/etc
    install -Dm644 COPYING /out/share/licenses/alsa-utils/COPYING
  |||);
{ sndfile: sndfile, pulse: pulse, utils: utils }

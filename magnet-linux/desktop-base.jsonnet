// Shared desktop hardware, terminal and fonts, without a compositor.
local base = (import './system.jsonnet').system;
local linux = import './kernel.jsonnet';
local libs = import '../packages/desktop-libs.jsonnet';
{
  system: base {
    name: 'magnet-linux-desktop',
    runDeps: [p for p in base.runDeps if p.name != linux.linux.name] +
             [linux.desktop, (import '../packages/foot.jsonnet').foot,
              libs.fonts, libs.ncurses, libs.seatd, libs.eudev],
  },
}

// Shared libuuid identity is preserved for existing desktop packages.
local b = import './hyprland/build.libsonnet';
local uuid = b.autotools('util-linux', '--disable-all-programs --enable-libuuid --disable-nls --without-python --without-systemd --without-systemdsystemunitdir', [], [], '', |||
  rm -rf /out/share/bash-completion
|||) { name: 'libuuid-2.41.2' };
{ uuid: uuid }

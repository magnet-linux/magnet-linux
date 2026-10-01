// Distribution kernel profiles use the package tree's generic constructor.
local kernel = (import '../packages/linux.libsonnet').kernel;
{
  linux: kernel(importstr './kernel.config', 'linux-6.15.7'),
  desktop: kernel(
    (importstr './kernel.config') + '\n' +
    (importstr './kernel-desktop.config'), 'linux-desktop-6.15.7') {
      // cfg80211 verifies the signed wireless regulatory database.
      buildDeps+: [(import '../packages/openssl.jsonnet').openssl,
                   (import '../packages/desktop-tools.jsonnet').pkgconf],
    },
}

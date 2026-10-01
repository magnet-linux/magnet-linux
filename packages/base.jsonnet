local core = import './core.jsonnet';
local tools = import './base/tools.jsonnet';
{
  base: {
    name: 'magnet-base', build: { kind: 'none' }, buildDeps: [], fetch: [],
    runDeps: [
      tools.shell_tools, tools.util_linux, tools.shadow, tools.procps,
      tools.iproute2, tools.dhcpcd, (import './base/init.jsonnet').sinit,
      tools.nano, tools.inetutils,
      tools.bash, core.tar, core.gzip, core.xz, core.diffutils,
      (import './browser-common/libraries.jsonnet').curl,
      (import './doas.jsonnet').doas,
      (import './base/manuals.jsonnet').mandoc,
    ],
  },
}

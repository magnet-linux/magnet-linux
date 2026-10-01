local base = (import '../packages/base.jsonnet').base;
local linux = (import './kernel.jsonnet').linux;
local btrfs = (import '../packages/btrfs.jsonnet').btrfs;
local openssh = (import '../packages/openssh.jsonnet').openssh;
local magpkg = (import '../packages/magpkg.jsonnet').magpkg;

{
  system: {
    name: 'magnet-linux',
    build: { kind: 'none' },
    runDeps: [base, btrfs, linux, openssh, magpkg,
              (import '../packages/chrony.jsonnet').chrony],
    buildDeps: [],
    fetch: [],
  },
}

// Optional source-cache seed for an unavailable GNU origin. Fetch exactly the
// same SHA-256-pinned bytes without changing the normal recipes or package IDs.
local core = import '../packages/core.jsonnet';
local names = ['make', 'binutils', 'gcc', 'coreutils', 'gawk', 'sed',
               'findutils', 'diffutils', 'bash', 'gzip', 'tar', 'grep'];
local kernel = import '../packages/kernel-tools.jsonnet';
local manifests = [import '../packages/base/sources.libsonnet',
                   import '../packages/desktop-sources.libsonnet',
                   import '../packages/browser-common/sources.libsonnet'];
local sources = std.flattenArrays([core[name].fetch for name in names]) +
                kernel.m4.fetch + kernel.bison.fetch +
                std.flattenArrays([[manifest[name].archive
                                    for name in std.objectFields(manifest)]
                                   for manifest in manifests]);
local origin = 'https://ftp.gnu.org/gnu/';
local mirror = std.extVar('gnu-mirror'); // Include the trailing slash.
{
  seed: {
    name: 'gnu-source-mirror',
    build: { kind: 'none' }, buildDeps: [], runDeps: [],
    fetch: [source + {
      urls: [std.strReplace(url, origin, mirror) for url in source.urls],
    } for source in sources if std.length([
      url for url in source.urls if std.startsWith(url, origin)
    ]) > 0],
  },
}

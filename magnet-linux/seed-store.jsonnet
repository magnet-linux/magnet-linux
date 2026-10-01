// Import a local seed into a new installation host's source cache through the
// normal verified fetch path. The installed system uses /var/lib/magpkg instead.
local seed = (import '../packages/bootstrap.jsonnet').bootstrap_seed;
{
  seed: seed + {
    name: 'local-bootstrap-source',
    fetch: [seed.fetch[0] + { urls: [std.extVar('bootstrap-url')] }],
  },
}

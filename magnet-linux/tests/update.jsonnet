local base = import '../system.jsonnet';
local hello = (import '../../packages/hello.jsonnet').hello;
{
  base: base.system,
  system: base.system + { runDeps+: [hello] },
}

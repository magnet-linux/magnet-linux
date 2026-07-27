local bootstrap_packages = import './bootstrap.jsonnet';
local bootstrap = bootstrap_packages.bootstrap;
local root_layout = bootstrap_packages.root_layout;
local build_policy = {
  buildEnv: {
    PATH: '/bin',
  },
};

{
  hello: build_policy + {
    name: 'hello',
    build: |||
      cat > hello.c <<'EOF'
      #include <stdio.h>

      int main(void) {
          puts("hello world");
          return 0;
      }
      EOF

      mkdir -p /out/bin
      cc -static -Os -s -o /out/bin/hello hello.c
    |||,
    runDeps: [root_layout],
    buildDeps: [bootstrap],
    fetch: [],
  },
}

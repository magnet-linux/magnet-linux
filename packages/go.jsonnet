local bootstrap = (import './bootstrap.jsonnet').bootstrap;
local root_layout = (import './bootstrap.jsonnet').root_layout;

{
  // Initial Go seed: the official Linux/amd64 toolchain. Its go/compile/link
  // binaries run without a glibc runtime. Consumers can build static Go programs
  // with CGO_ENABLED=0 in the musl build environment.
  // TODO: rebuild Go from source using this binary only as the bootstrap seed.
  go: {
    name: 'go-1.26.4',
    buildEnv: { PATH: '/bin' },
    build: { kind: 'script', script: |||
      mkdir -p /out/lib /out/bin
      tar --no-same-owner -xzf /fetch/go.tar.gz -C /out/lib
      ln -s ../lib/go/bin/go /out/bin/go
      ln -s ../lib/go/bin/gofmt /out/bin/gofmt
    ||| },
    buildDeps: [bootstrap],
    runDeps: [root_layout],
    fetch: [{
      filename: 'go.tar.gz',
      sha256: '1153d3d50e0ac764b447adfe05c2bcf08e889d42a02e0fe0259bd47f6733ad7f',
      urls: ['https://go.dev/dl/go1.26.4.linux-amd64.tar.gz'],
    }],
  },
}

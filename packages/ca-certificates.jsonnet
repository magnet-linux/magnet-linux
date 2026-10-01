local bootstrap = (import './bootstrap.jsonnet').bootstrap;
local root_layout = (import './bootstrap.jsonnet').root_layout;

{
  ca_certificates: {
    name: 'ca-certificates-2026-09-25',
    buildEnv: { PATH: '/bin' },
    build: { kind: 'script', script: |||
      mkdir -p /out/etc/ssl/certs
      cp /fetch/cacert.pem /out/etc/ssl/certs/ca-certificates.crt
    ||| },
    buildDeps: [bootstrap],
    runDeps: [root_layout],
    fetch: [{
      filename: 'cacert.pem',
      sha256: 'a41b5d356aea97a529fe27e0f7316d2f9d946d75927476cf9cf1b90637d00505',
      urls: ['https://curl.se/ca/cacert-2026-09-25.pem'],
    }],
  },
}

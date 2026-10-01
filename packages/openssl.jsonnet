local bootstrap = (import './bootstrap.jsonnet').bootstrap;
local perl = (import './kernel-tools.jsonnet').perl;
{
  openssl: {
    name: 'openssl-3.5.8',
    buildEnv: { PATH: '/bin' },
    build: { kind: 'script', script: |||
      tar --no-same-owner -xzf /fetch/openssl.tar.gz
      cd openssl-3.5.8
      ./Configure linux-x86_64 --prefix=/ --libdir=lib --openssldir=/etc/ssl \
        no-shared no-module no-tests
      make -j"$BUILD_PARALLELISM" build_libs
      make DESTDIR=/out install_dev
      mkdir -p /out/share/licenses/openssl
      cp LICENSE.txt /out/share/licenses/openssl/
    ||| },
    buildDeps: [bootstrap, perl],
    runDeps: [],
    fetch: [{
      filename: 'openssl.tar.gz',
      sha256: 'a8f84a39918ec6415ce765d9b429d313ba97b8143169c172e734b9514464f5b2',
      urls: ['https://www.openssl.org/source/openssl-3.5.8.tar.gz'],
    }],
  },
}

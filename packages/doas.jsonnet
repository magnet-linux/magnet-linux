local b = (import './browser-common/build.libsonnet') {
  sources+: {
    opendoas: {
      version: '6.8.2', directory: 'opendoas-6.8.2',
      archive: {
        filename: 'opendoas-6.8.2.tar.xz',
        sha256: '4e98828056d6266bd8f2c93e6ecf12a63a71dbfd70a5ea99ccd4ab6d0745adf0',
        urls: ['https://github.com/Duncaen/OpenDoas/releases/download/v6.8.2/opendoas-6.8.2.tar.xz'],
      },
    },
  },
};
{
  doas: b.package('opendoas', |||
    ./configure --prefix=/ --bindir=/bin --sysconfdir=/etc \
      --with-pam --without-shadow --without-timestamp
    grep -q '^#define USE_PAM' config.h
    make -j"$BUILD_PARALLELISM" YACC='bison -y'
    # Keep editable policy and PAM configuration outside package ownership.
    install -Dm4755 doas /out/bin/doas
    install -Dm644 doas.1 /out/share/man/man1/doas.1
    install -Dm644 doas.conf.5 /out/share/man/man5/doas.conf.5
    install -Dm644 LICENSE /out/share/licenses/opendoas/LICENSE
  |||, [(import './login/login.jsonnet').pam],
    [(import './kernel-tools.jsonnet').bison, (import './kernel-tools.jsonnet').m4]),
}

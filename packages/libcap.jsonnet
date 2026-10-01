local b = (import './browser-common/build.libsonnet') {
  sources+: {
    libcap: {
      version: '2.78', directory: 'libcap-2.78',
      archive: (import './bubblewrap.jsonnet').bubblewrap.fetch[1],
    },
  },
};
{
  libcap: b.package('libcap', |||
    make -C libcap -j"$BUILD_PARALLELISM" prefix=/ lib=lib PAM_CAP=no \
      GOLANG=no PTHREADS=no USE_GPERF=no
    make -C libcap DESTDIR=/out prefix=/ lib=lib PAM_CAP=no \
      GOLANG=no PTHREADS=no USE_GPERF=no RAISE_SETFCAP=no install
    rm -f /out/lib/libcap.a
    install -Dm644 License /out/share/licenses/libcap/License
  |||),
}

local bootstrap = (import './bootstrap.jsonnet').bootstrap;
{
  tzdata: {
    name: 'tzdata-2026e',
    buildEnv: { PATH: '/bin', CC: 'gcc' },
    build: { kind: 'script', script: |||
      mkdir tzdb
      cd tzdb
      tar --no-same-owner -xf /fetch/tzcode2026e.tar.gz
      tar --no-same-owner -xf /fetch/tzdata2026e.tar.gz
      make -j"$BUILD_PARALLELISM" CFLAGS=-O2 zic tzdata.zi
      mkdir -p /out/share/zoneinfo /out/share/licenses/tzdata
      ./zic -b slim -d /out/share/zoneinfo tzdata.zi
      cp tzdata.zi zone.tab zone1970.tab zonenow.tab iso3166.tab \
        leapseconds leap-seconds.list /out/share/zoneinfo/
      cp LICENSE /out/share/licenses/tzdata/
      test -f /out/share/zoneinfo/Etc/UTC
      test -f /out/share/zoneinfo/Pacific/Auckland
    ||| },
    buildDeps: [bootstrap],
    runDeps: [],
    fetch: [
      {
        filename: 'tzcode2026e.tar.gz',
        sha256: 'cc3d27ca2a0d8399504551b920970d80af83bfb9c216e8082a15491921935d54',
        urls: ['https://data.iana.org/time-zones/releases/tzcode2026e.tar.gz'],
      },
      {
        filename: 'tzdata2026e.tar.gz',
        sha256: 'b26882805f26aac59d5b222978e6580484b834ccdc98be89df2f05a6dc53a652',
        urls: ['https://data.iana.org/time-zones/releases/tzdata2026e.tar.gz'],
      },
    ],
  },
}

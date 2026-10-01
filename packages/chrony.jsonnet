local b = (import './browser-common/build.libsonnet') {
  sources+: import './chrony-sources.libsonnet',
};
{
  chrony: b.package('chrony', |||
    ./configure --prefix=/ --sbindir=/bin --sysconfdir=/etc \
      --localstatedir=/var --with-user=chrony --with-chronyc-user=chrony \
      --with-pidfile=/run/chrony/chronyd.pid \
      --disable-readline --disable-nts --without-seccomp
    grep -q '^#define FEAT_PRIVDROP' config.h
    make -j"$BUILD_PARALLELISM"
    make DESTDIR=/out install
    # Machine state and editable settings are provisioned separately.
    rm -rf /out/var /out/etc
    install -Dm644 COPYING /out/share/licenses/chrony/COPYING
    /out/bin/chronyd --version
    /out/bin/chronyc --version
  |||, [(import './libcap.jsonnet').libcap]),
}

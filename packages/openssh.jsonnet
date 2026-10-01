local bootstrap = import './bootstrap.jsonnet';
local openssl = (import './openssl.jsonnet').openssl;
local zlib = (import './zlib.jsonnet').zlib;
{
  openssh: {
    name: 'openssh-10.5p1',
    buildEnv: { PATH: '/bin' },
    build: { kind: 'script', script: |||
      tar --no-same-owner -xzf /fetch/openssh.tar.gz
      cd openssh-10.5p1
      CFLAGS='-O2 -std=gnu17' LDFLAGS=-static ./configure \
        --prefix=/ --sbindir=/bin --sysconfdir=/etc/ssh \
        --libexecdir=/libexec/openssh --with-default-path=/bin \
        --with-superuser-path=/bin --with-privsep-user=sshd \
        --with-privsep-path=/run/sshd --with-pid-dir=/run \
        --with-sandbox=seccomp_filter --without-pam
      make -j"$BUILD_PARALLELISM"
      make DESTDIR=/out install-nosysconf
      # Runtime state and machine configuration are initialized separately.
      rm -rf /out/run
      mkdir -p /out/share/openssh /out/share/licenses/openssh
      cp moduli /out/share/openssh/moduli
      cp LICENCE /out/share/licenses/openssh/
      cp /share/licenses/openssl/LICENSE.txt /out/share/licenses/openssh/OPENSSL-LICENSE.txt
    ||| },
    buildDeps: [bootstrap.bootstrap, openssl, zlib],
    runDeps: [bootstrap.root_layout],
    fetch: [{
      filename: 'openssh.tar.gz',
      sha256: 'd44d28a839ea9daf969cc69150fde59910b2b39361dad81a3bd6cbd19218db11',
      urls: ['https://cdn.openbsd.org/pub/OpenBSD/OpenSSH/portable/openssh-10.5p1.tar.gz'],
    }],
  },
}

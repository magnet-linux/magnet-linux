local bootstrap = import './bootstrap.jsonnet';
local tools = import './kernel-tools.jsonnet';
local libelf = (import './libelf.jsonnet').libelf;
local zlib = (import './zlib.jsonnet').zlib;
local kernel(config, name) = {
    name: name,
    buildEnv: { PATH: '/bin' },
    build: { kind: 'script', script: |||
      tar --no-same-owner -xJf /fetch/linux.tar.xz
      cd linux-6.15.7
      export KBUILD_BUILD_USER=magnet KBUILD_BUILD_HOST=magnet
      # BusyBox date must parse this too: gen_initramfs falls back to the wall
      # clock when it cannot parse the timestamp, making kernels differ.
      export KBUILD_BUILD_TIMESTAMP='1970-01-01 00:00:00'
      export KBUILD_BUILD_VERSION=1
      cat > magnet.config <<'MAGNET_CONFIG'
    ||| + config + |||
      MAGNET_CONFIG
      make KCONFIG_ALLCONFIG=magnet.config allnoconfig
      # Kconfig silently drops requests with unmet dependencies. Fail before
      # compilation if the profile did not actually select a requested option.
      while IFS= read -r option; do
        case "$option" in
          CONFIG_*) grep -Fqx "$option" .config || { echo "Unselected kernel option: $option" >&2; exit 1; };;
        esac
      done < magnet.config
      # Our libelf is static, so objtool must also link its zlib dependency.
      make -j"$BUILD_PARALLELISM" HOSTLDFLAGS=-static LIBELF_LIBS='-lelf -lz' bzImage
      mkdir -p /out/boot /out/share/licenses/linux
      cp arch/x86/boot/bzImage /out/boot/vmlinuz
      cp .config /out/boot/kernel.config
      make -s kernelrelease > /out/boot/kernel.release
      cp COPYING /out/share/licenses/linux/
    ||| },
    runDeps: [bootstrap.root_layout],
    buildDeps: [bootstrap.bootstrap, tools.m4, tools.flex, tools.bison, tools.perl, zlib, libelf],
    fetch: [{
      filename: 'linux.tar.xz',
      sha256: '3507dd105b0a0e1101bd43d294472fccf853429a259a5fa7c67467bba318f8e9',
      urls: ['https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-6.15.7.tar.xz'],
    }],
  };
{
  linux: kernel(importstr '../magnet-linux/kernel.config', 'linux-6.15.7'),
  desktop: kernel(
    (importstr '../magnet-linux/kernel.config') + '\n' +
    (importstr '../magnet-linux/kernel-desktop.config'), 'linux-desktop-6.15.7') {
      // cfg80211 verifies the signed wireless regulatory database.
      buildDeps+: [(import './openssl.jsonnet').openssl,
                   (import './desktop-tools.jsonnet').pkgconf],
    },
}

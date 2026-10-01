local bootstrap = (import './bootstrap.jsonnet').bootstrap;
local zlib = (import './zlib.jsonnet').zlib;
{
  libelf: {
    name: 'libelf',
    buildEnv: { PATH: '/bin' },
    build: { kind: 'script', script: |||
      tar --no-same-owner -xjf /fetch/elfutils.tar.bz2
      cd elfutils-0.192
      # Build only libelf.a. The top-level configure also checks dependencies
      # of the elfutils CLI programs, which this package does not compile.
      ac_cv_search_argp_parse='none required' \
      ac_cv_search_fts_close='none required' \
      ac_cv_search__obstack_free='none required' \
      CFLAGS='-O2 -std=gnu17' ./configure --prefix=/ --disable-nls \
        --disable-debuginfod --disable-libdebuginfod --disable-werror \
        --without-bzlib --without-lzma --without-zstd
      make -C libelf -j"$BUILD_PARALLELISM" libelf.a
      # libelf uses the search-tree helper from libeu. Include that object in
      # the static archive without building the unrelated CLI support code.
      make -C lib eu-search.o
      ar rcs libelf/libelf.a lib/eu-search.o
      mkdir -p /out/lib /out/include /out/share/licenses/libelf
      cp libelf/libelf.a /out/lib/
      cp libelf/libelf.h libelf/gelf.h libelf/nlist.h /out/include/
      cp COPYING-GPLV2 COPYING-LGPLV3 /out/share/licenses/libelf/
    ||| },
    runDeps: [],
    buildDeps: [bootstrap, zlib],
    fetch: [{
      filename: 'elfutils.tar.bz2',
      sha256: '616099beae24aba11f9b63d86ca6cc8d566d968b802391334c91df54eab416b4',
      urls: ['https://sourceware.org/elfutils/ftp/0.192/elfutils-0.192.tar.bz2'],
    }],
  },
}

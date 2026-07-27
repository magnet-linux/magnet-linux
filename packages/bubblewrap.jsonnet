local bootstrap_packages = import './bootstrap.jsonnet';
local bootstrap = bootstrap_packages.bootstrap;
local root_layout = bootstrap_packages.root_layout;
local build_policy = {
  buildEnv: {
    PATH: '/bin',
  },
};

local bubblewrap = build_policy + {
  name: 'bubblewrap',
  build: |||
    tar -xJf /fetch/libcap-2.78.tar.xz
    tar -xJf /fetch/bubblewrap-0.11.2.tar.xz

    capdir="$PWD/libcap-2.78/libcap"
    (
      cd "$capdir"
      grep -E \
        '^#define\s+CAP_([^\s]+)\s+[0-9]+\s*$' \
        include/uapi/linux/capability.h |
        sed \
          -e 's/^#define\s\+/{"/' \
          -e 's/\s*$/},/' \
          -e 's/\s\+/",/' \
          -e 'y/ABCDEFGHIJKLMNOPQRSTUVWXYZ/abcdefghijklmnopqrstuvwxyz/' \
          > cap_names.list.h
      gcc -O2 -Iinclude _makenames.c -o _makenames
      ./_makenames > cap_names.h
      gcc \
        -O2 \
        -fPIC \
        -D_LIBPSX_PTHREAD_LINKAGE \
        -I. \
        -Iinclude \
        -c \
        cap_alloc.c \
        cap_proc.c \
        cap_extint.c \
        cap_flag.c \
        cap_text.c \
        cap_file.c \
        cap_syscalls.c
      ar rcs libcap.a \
        cap_alloc.o \
        cap_proc.o \
        cap_extint.o \
        cap_flag.o \
        cap_text.o \
        cap_file.o \
        cap_syscalls.o
    )

    cd bubblewrap-0.11.2
    cat > config.h <<'EOF'
    #define PACKAGE_STRING "bubblewrap 0.11.2"
    EOF

    mkdir -p /out/bin
    gcc \
      -static \
      -O2 \
      -D_GNU_SOURCE \
      -I. \
      -I"$capdir/include" \
      bubblewrap.c \
      bind-mount.c \
      network.c \
      utils.c \
      "$capdir/libcap.a" \
      -o /out/bin/bwrap
    strip /out/bin/bwrap
  |||,
  runDeps: [root_layout],
  buildDeps: [bootstrap],
  fetch: [
    {
      filename: 'bubblewrap-0.11.2.tar.xz',
      sha256: '69abc30005d2186baf7737feacd8da35633b93cf5af38838ecff17c5f8e924f6',
      urls: [
        'https://github.com/containers/bubblewrap/releases/download/v0.11.2/bubblewrap-0.11.2.tar.xz',
      ],
    },
    {
      filename: 'libcap-2.78.tar.xz',
      sha256: '0d621e562fd932ccf67b9660fb018e468a683d7b827541df27813228c996bb11',
      urls: [
        'https://www.kernel.org/pub/linux/libs/security/linux-privs/libcap2/libcap-2.78.tar.xz',
      ],
    },
  ],
};

{
  bubblewrap: bubblewrap,
}

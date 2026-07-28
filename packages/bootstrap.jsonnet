local build_policy = {
  buildEnv: {
    PATH: '/bin',
  },
};

local bootstrap_seed = build_policy + {
  name: 'bootstrap-seed',
  build: { kind: 'unpack' },
  runDeps: [],
  buildDeps: [],
  fetch: [
    {
      filename: 'bootstrap.tar.zst',
      sha256: '2703bd4a9bd9fbdddb00cca71cbcced18b769e338df099c8809f28070d50316a',
      urls: [
        'file:///home/ac/src/magnet-linux/bootstrap/out/bootstrap.tar.zst',
      ],
    },
  ],
};

// The filesystem hierarchy is ordinary package policy. Other package trees
// can provide a different layout without teaching the store about it.
local root_layout = build_policy + {
  name: 'root-layout',
  build: { kind: 'script', script: |||
    mkdir -p \
      /out/bin \
      /out/boot \
      /out/dev \
      /out/etc \
      /out/home \
      /out/include \
      /out/lib \
      /out/libexec \
      /out/mnt \
      /out/proc \
      /out/root \
      /out/run \
      /out/share \
      /out/tmp \
      /out/var

    chmod 0700 /out/root
    chmod 1777 /out/tmp

    # Relative links remain inside an extracted package root.
    ln -s . /out/usr
    ln -s bin /out/sbin
  ||| },
  runDeps: [],
  buildDeps: [bootstrap_seed],
  fetch: [],
};

// Normalize the stable seed into the root-prefix layout without changing the
// seed archive itself. The layout is kept separate so it remains visible as a
// dependency and can evolve independently from the tool payload.
local bootstrap_tools = build_policy + {
  name: 'bootstrap-tools',
  build: { kind: 'script', script: |||
    copy_into() {
      source="$1"
      destination="$2"
      if test -e "$source"; then
        mkdir -p "$destination"
        cp -a "$source"/. "$destination"/
      fi
    }

    copy_into /bin /out/bin
    copy_into /sbin /out/bin
    copy_into /usr/bin /out/bin
    copy_into /usr/sbin /out/bin

    copy_into /include /out/include
    copy_into /lib /out/lib
    copy_into /libexec /out/libexec
    copy_into /share /out/share

    copy_into /etc /out/etc
    copy_into /x86_64-linux-musl /out/x86_64-linux-musl
    cp -a /linuxrc /out/linuxrc
  ||| },
  runDeps: [],
  buildDeps: [bootstrap_seed],
  fetch: [],
};

// This empty meta-package gives consumers one dependency whose runtime
// closure installs the layout first and then the normalized bootstrap tools.
local bootstrap = build_policy + {
  name: 'bootstrap',
  build: { kind: 'none' },
  runDeps: [root_layout, bootstrap_tools],
  buildDeps: [root_layout, bootstrap_tools],
  fetch: [],
};

{
  bootstrap_seed: bootstrap_seed,
  root_layout: root_layout,
  bootstrap_tools: bootstrap_tools,
  bootstrap: bootstrap,
}

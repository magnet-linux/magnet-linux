local bootstrap_packages = import './bootstrap.jsonnet';
local bootstrap = bootstrap_packages.bootstrap;
local build_policy = {
  buildEnv: {
    PATH: '/bin',
  },
};

local shell_launcher = build_policy + {
  name: 'shell-launcher',
  build: { kind: 'script', script: |||
    mkdir -p /out/libexec
    cat > /out/libexec/magpkg-shell <<'EOF'
    #!/bin/sh
    set -eu

    root=$1
    shift

    working_directory=$PWD
    home_directory=${HOME:-$working_directory}

    run_shell() {
      exec bwrap \
        --die-with-parent \
        --unshare-all \
        --share-net \
        --hostname magpkg-shell \
        --overlay-src "$root" \
        --tmp-overlay / \
        --dev-bind /dev /dev \
        --proc /proc \
        --bind /tmp /tmp \
        --ro-bind-try /etc /etc \
        --ro-bind-try /nix /nix \
        --ro-bind-try /opt /opt \
        --ro-bind-try /run /run \
        --ro-bind-try /srv /srv \
        --ro-bind-try /sys /sys \
        --ro-bind-try /var /var \
        --dir "$home_directory" \
        --bind "$home_directory" "$home_directory" \
        --dir "$working_directory" \
        --bind "$working_directory" "$working_directory" \
        --setenv PATH /bin:/usr/bin \
        --setenv SHELL /bin/sh \
        --chdir "$working_directory" \
        "$@"
    }

    if test "$#" -eq 0; then
      run_shell /bin/sh -i
    fi
    run_shell "$@"
    EOF
    chmod 0755 /out/libexec/magpkg-shell
  ||| },
  runDeps: [],
  buildDeps: [bootstrap],
  fetch: [],
};

local shell = build_policy + {
  name: 'shell',
  build: { kind: 'none' },
  runDeps: [bootstrap, shell_launcher],
  buildDeps: [bootstrap, shell_launcher],
  fetch: [],
};

{
  shell: shell,
}

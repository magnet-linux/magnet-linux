# Generic bootstrap seed

`host/build.sh` builds a generic `x86_64-linux-musl` bootstrap root containing
musl, BusyBox, GCC with C++, binutils, GNU awk, Make, GNU patch, GNU tar, and
zstd. It builds the root three times, requires the last two canonical archives
to be identical, and writes `bootstrap/out/bootstrap.tar.zst`.

The host must be x86-64 Linux and provide the commands checked near the top of
`host/build.sh`. A pinned environment is also available:

```sh
nix-shell bootstrap/shell.nix --pure --run ./bootstrap/host/build.sh
```

Downloaded source archives are cached in `bootstrap/work/distfiles` and
verified against `bootstrap/sources`. Builds run without network access.
`bootstrap/bootstrap.sha256` is never updated by the build; it is filled only
after independent builds agree.

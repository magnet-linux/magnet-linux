let
  nixpkgs = builtins.fetchTarball {
    url = "https://github.com/NixOS/nixpkgs/archive/a50de1b7d8a586adc18d2395c19de7d6058e6030.tar.gz";
    sha256 = "1ks8s77y6021ryqfmw2qayqhnij2yrxl81yh1lbk51cjkbd6vjds";
  };
  pkgs = import nixpkgs {
    config = {};
    overlays = [];
  };
in
pkgs.mkShellNoCC {
  hardeningDisable = [ "all" ];

  packages = with pkgs; [
    bash
    binutils
    bison
    bubblewrap
    bzip2
    cacert
    coreutils
    curl
    diffutils
    file
    findutils
    flex
    gawk
    gcc
    gnugrep
    gnumake
    gnused
    gnutar
    gzip
    patch
    rsync
    xz
    zstd
  ];
}

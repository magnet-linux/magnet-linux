# Host tools only; installed packages are built separately by magpkg.
{ pkgs ? import <nixpkgs> {} }:
pkgs.mkShellNoCC {
  packages = with pkgs; [
    bubblewrap qemu_kvm btrfs-progs dosfstools util-linux
    grub2_efi coreutils gnutar zstd bash shellcheck openssh
    scrot xdotool xorg-server python3
  ];
  OVMF_CODE = "${pkgs.OVMF.fd}/FV/OVMF_CODE.fd";
  OVMF_VARS = "${pkgs.OVMF.fd}/FV/OVMF_VARS.fd";
}

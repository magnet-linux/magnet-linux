#!/usr/bin/env bash
set -euo pipefail
distro=$(dirname "$(realpath "${BASH_SOURCE[0]}")")
case "${1:-help}" in
  install) shift; exec "$distro/install.sh" "$@";;
  help|--help|-h)
    echo 'usage: magnet-linux install --root MOUNTPOINT [options]'
    echo 'Install from booted media into a prepared ext4 or Btrfs root.'
    echo 'Run magnet-linux install --help for options.'
    ;;
  *) echo "Unknown command: $1" >&2; exit 2;;
esac

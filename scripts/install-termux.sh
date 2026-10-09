#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${PREFIX:-}" || ! -d "${PREFIX}/bin" ]]; then
  echo "Run this installer inside Termux." >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
install -m 0755 "$script_dir/oberon" "$PREFIX/bin/oberon"
printf 'Installed Oberon to %s/bin/oberon\n' "$PREFIX"
printf 'Run: oberon --version\n'

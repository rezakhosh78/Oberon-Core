#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${PREFIX:-}" || ! -d "${PREFIX}/bin" ]]; then
  echo "Run this installer inside Termux." >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
binary="$script_dir/oberon"
if [[ ! -x "$binary" ]]; then
  echo "The Termux archive does not contain an executable Oberon binary." >&2
  exit 1
fi

help_output="$("$binary" --help 2>&1 || true)"
if [[ "$help_output" != *"Commands:"* || "$help_output" != *"connect"* ]]; then
  echo "This Termux archive contains an outdated binary without the proxy CLI. Download a newly built Termux release." >&2
  exit 1
fi

install -m 0755 "$binary" "$PREFIX/bin/oberon"
printf 'Installed Oberon to %s/bin/oberon\n' "$PREFIX"
printf 'Run: oberon --help\n'

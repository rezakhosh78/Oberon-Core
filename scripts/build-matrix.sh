#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${1:-$root/dist}"
mkdir -p "$out"

# Build targets supported by the included userspace tunnel and Go toolchain.
targets=(
  'linux amd64' 'linux arm64' 'linux arm' 'linux 386' 'linux riscv64'
  'windows amd64' 'windows arm64' 'windows 386'
  'darwin amd64' 'darwin arm64'
  'freebsd amd64' 'freebsd arm64'
  'openbsd amd64' 'openbsd arm64' 'openbsd 386'
)

for target in "${targets[@]}"; do
  read -r goos goarch <<< "$target"
  export GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0
  if [[ "$goarch" == arm && "$goos" == linux ]]; then export GOARM=7; else unset GOARM || true; fi
  suffix=''
  [[ "$goos" == windows ]] && suffix='.exe'
  echo "Building $goos/$goarch"
  (cd "$root" && go build -trimpath -o "$out/oberon-$goos-$goarch$suffix" .)
done

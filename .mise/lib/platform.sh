#!/usr/bin/env bash
# Shared platform detection and build variable setup.
# Source this file from mise tasks: source .mise/lib/platform.sh

set -euo pipefail

UNAME_S="${UNAME_S:-$(uname -s)}"
UNAME_M="${UNAME_M:-$(uname -m)}"

# Platform
case "$UNAME_S" in
  Linux)   PLATFORM=linux ;;
  Darwin)  PLATFORM=darwin ;;
  *)       PLATFORM="$UNAME_S" ;;
esac

# Architecture
case "$UNAME_M" in
  x86_64)      ARCH=amd64 ;;
  aarch64|armv81) ARCH=arm64 ;;
  loongarch64) ARCH=loong64 ;;
  riscv64)     ARCH=riscv64 ;;
  mipsel)      ARCH=mipsle ;;
  *)           ARCH="$UNAME_M" ;;
esac

# Binary settings
BINARY_NAME=picoclaw
BUILD_DIR=build
CMD_DIR="cmd/${BINARY_NAME}"
EXT=""
LNCMD="ln -sf"

# Windows detection (Git Bash / MSYS2 / Cygwin)
case "$UNAME_S" in
  MINGW*|MSYS*|CYGWIN*) EXT=".exe"; LNCMD="cp" ;;
esac
if [ "$UNAME_S" = "windows" ]; then
  EXT=".exe"
fi

BINARY_PATH="${BUILD_DIR}/${BINARY_NAME}-${PLATFORM}-${ARCH}"

# Go settings
GO_BUILD_TAGS="${GO_BUILD_TAGS:-goolm,stdjson}"
GOFLAGS_CUSTOM="-v -tags ${GO_BUILD_TAGS}"
# Build tags without goolm (for mipsle builds)
GO_BUILD_TAGS_NO_GOOLM=$(echo "$GO_BUILD_TAGS" | tr ',' '\n' | grep -v '^goolm$' | paste -sd ',' -)
GOFLAGS_NO_GOOLM="-v -tags ${GO_BUILD_TAGS_NO_GOOLM}"

# Version info
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
GIT_COMMIT=$(git rev-parse --short=8 HEAD 2>/dev/null || echo "dev")
BUILD_TIME=$(date +%FT%T%z)
GO_VERSION=$(go version | awk '{print $3}')
CONFIG_PKG=github.com/sipeed/picoclaw/pkg/config
LDFLAGS="-X ${CONFIG_PKG}.Version=${VERSION} -X ${CONFIG_PKG}.GitCommit=${GIT_COMMIT} -X ${CONFIG_PKG}.BuildTime=${BUILD_TIME} -X ${CONFIG_PKG}.GoVersion=${GO_VERSION} -s -w"

# Installation
INSTALL_PREFIX="${INSTALL_PREFIX:-$HOME/.local}"
INSTALL_BIN_DIR="${INSTALL_PREFIX}/bin"
INSTALL_TMP_SUFFIX=".new"

# Workspace
PICOCLAW_HOME="${PICOCLAW_HOME:-$HOME/.picoclaw}"
WORKSPACE_DIR="${WORKSPACE_DIR:-$PICOCLAW_HOME/workspace}"

# Golangci-lint
GOLANGCI_LINT="${GOLANGCI_LINT:-golangci-lint}"

# WEB_GO: on Darwin, enable CGO with macOS version flags
web_go() {
  if [ "$PLATFORM" = "darwin" ]; then
    CGO_LDFLAGS="-mmacosx-version-min=10.11" \
    CGO_CFLAGS="-mmacosx-version-min=10.11" \
    CGO_ENABLED=1 go "$@"
  else
    CGO_ENABLED=0 go "$@"
  fi
}

# Patch MIPS LE ELF e_flags for NaN2008-only kernels
patch_mips_flags() {
  local binary="$1"
  if [ -f "$binary" ]; then
    printf '\004\024\000\160' | dd of="$binary" bs=1 seek=36 count=4 conv=notrunc 2>/dev/null || \
      { echo "Error: failed to patch MIPS e_flags for $binary"; exit 1; }
  else
    echo "Error: $binary not found, cannot patch MIPS e_flags"; exit 1
  fi
}

# Patch creack/pty for loong64 support
patch_pty_loong64() {
  local pty_dir
  pty_dir="$(go env GOMODCACHE)/github.com/creack/pty@v1.1.9"
  if [ -d "$pty_dir" ] && [ ! -f "$pty_dir/ztypes_loong64.go" ]; then
    chmod +w "$pty_dir" 2>/dev/null || true
    printf '//go:build linux && loong64\npackage pty\ntype (_C_int int32; _C_uint uint32)\n' > "$pty_dir/ztypes_loong64.go"
  fi
}

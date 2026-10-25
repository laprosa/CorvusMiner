#!/usr/bin/env bash
# Installs the toolchain needed to build the CorvusMiner Windows client from
# Linux, and to build the panel's `builder` binary.
#
#   cmake, make                  build system
#   mingw-w64 (x86_64-w64-mingw32-gcc / g++ / windres)   cross compiler
#   python3                      post-link PE patcher used by Client/CMakeLists.txt
#   go                           builds BuilderCLI -> ../builder
#
# Usage:
#   ./install-deps.sh            install whatever is missing (sudo if needed)
#   ./install-deps.sh --check    only report what is missing; install nothing
#   ./install-deps.sh --build    install, then build ../builder
#   ./install-deps.sh --help
set -euo pipefail

CHECK_ONLY=0
DO_BUILD=0

usage() {
    sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
}

for arg in "$@"; do
    case "$arg" in
        --check) CHECK_ONLY=1 ;;
        --build) DO_BUILD=1 ;;
        -h|--help) usage ;;
        *)
            echo "Unknown option: $arg" >&2
            echo "Try --help" >&2
            exit 2
            ;;
    esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ── Output helpers ───────────────────────────────────────────────────────────
if [ -t 1 ]; then
    C_INFO=$'\033[36m'; C_OK=$'\033[32m'; C_WARN=$'\033[33m'; C_ERR=$'\033[31m'; C_OFF=$'\033[0m'
else
    C_INFO=''; C_OK=''; C_WARN=''; C_ERR=''; C_OFF=''
fi

info() { printf '%s[INFO]%s %s\n' "$C_INFO" "$C_OFF" "$*"; }
ok()   { printf '%s[ OK ]%s %s\n' "$C_OK" "$C_OFF" "$*"; }
warn() { printf '%s[WARN]%s %s\n' "$C_WARN" "$C_OFF" "$*"; }
fail() { printf '%s[FAIL]%s %s\n' "$C_ERR" "$C_OFF" "$*"; }

# ── Required tools ───────────────────────────────────────────────────────────
CMDS=(
    cmake
    make
    x86_64-w64-mingw32-gcc
    x86_64-w64-mingw32-g++
    x86_64-w64-mingw32-windres
    python3
    go
)

# A readable name for each command, used in the report.
describe() {
    case "$1" in
        cmake) echo "CMake" ;;
        make) echo "GNU make" ;;
        x86_64-w64-mingw32-gcc) echo "MinGW-w64 C compiler" ;;
        x86_64-w64-mingw32-g++) echo "MinGW-w64 C++ compiler" ;;
        x86_64-w64-mingw32-windres) echo "MinGW-w64 resource compiler" ;;
        python3) echo "Python 3 (PE string patcher)" ;;
        go) echo "Go (builds BuilderCLI)" ;;
        *) echo "$1" ;;
    esac
}

version_of() {
    case "$1" in
        cmake) cmake --version 2>/dev/null | head -n1 ;;
        make) make --version 2>/dev/null | head -n1 ;;
        python3) python3 --version 2>/dev/null ;;
        go) go version 2>/dev/null ;;
        *) "$1" --version 2>/dev/null | head -n1 ;;
    esac
}

MISSING=()
for cmd in "${CMDS[@]}"; do
    command -v "$cmd" >/dev/null 2>&1 || MISSING+=("$cmd")
done

report() {
    echo
    info "Toolchain status"
    local missing=0
    for cmd in "${CMDS[@]}"; do
        if command -v "$cmd" >/dev/null 2>&1; then
            printf '  %s✓%s %-28s %s\n' "$C_OK" "$C_OFF" "$(describe "$cmd")" "$(version_of "$cmd")"
        else
            printf '  %s✗%s %-28s missing\n' "$C_ERR" "$C_OFF" "$(describe "$cmd")"
            missing=$((missing + 1))
        fi
    done
    echo
    return "$missing"
}

if [ "${#MISSING[@]}" -eq 0 ]; then
    ok "All build requirements are already installed."
    report || true
    if [ "$DO_BUILD" -eq 1 ]; then
        info "Building ../builder"
        ( cd "$SCRIPT_DIR" && go build -trimpath -ldflags="-s -w" -o ../builder . )
        ok "Built $(cd "$SCRIPT_DIR/.." && pwd)/builder"
    else
        info "Next: cd $(basename "$SCRIPT_DIR") && go build -trimpath -ldflags=\"-s -w\" -o ../builder ."
    fi
    exit 0
fi

if [ "$CHECK_ONLY" -eq 1 ]; then
    warn "Missing: ${MISSING[*]}"
    report || true
    echo "Run '$0' without --check to install them."
    exit 1
fi

# ── Install ──────────────────────────────────────────────────────────────────
SUDO=""
if [ "$(id -u)" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
    else
        fail "This script needs root (or sudo) to install packages."
        exit 1
    fi
fi

install_with() {
    local pm="$1"; shift
    info "Installing missing packages with $pm: $*"
    case "$pm" in
        apt)
            $SUDO apt-get update -qq
            DEBIAN_FRONTEND=noninteractive $SUDO apt-get install -y --no-install-recommends "$@"
            ;;
        dnf) $SUDO dnf install -y "$@" ;;
        yum) $SUDO yum install -y "$@" ;;
        pacman) $SUDO pacman -Sy --needed --noconfirm "$@" ;;
        zypper) $SUDO zypper --non-interactive install "$@" ;;
        apk) $SUDO apk add --no-cache "$@" ;;
        *)
            fail "Unsupported package manager: $pm"
            return 1
            ;;
    esac
}

if command -v apt-get >/dev/null 2>&1; then
    PKGS=(cmake build-essential mingw-w64 python3 golang-go)
    PM=apt
elif command -v dnf >/dev/null 2>&1; then
    PKGS=(cmake make gcc gcc-c++ mingw64-gcc mingw64-gcc-c++ mingw64-binutils python3 golang)
    PM=dnf
elif command -v yum >/dev/null 2>&1; then
    PKGS=(cmake make gcc gcc-c++ mingw64-gcc mingw64-gcc-c++ mingw64-binutils python3 golang)
    PM=yum
elif command -v pacman >/dev/null 2>&1; then
    PKGS=(cmake base-devel mingw-w64-gcc python go)
    PM=pacman
elif command -v zypper >/dev/null 2>&1; then
    PKGS=(cmake make gcc gcc-c++ mingw64-cross-gcc mingw64-cross-binutils python3 go)
    PM=zypper
elif command -v apk >/dev/null 2>&1; then
    PKGS=(cmake build-base mingw-w64-gcc python3 go)
    PM=apk
else
    fail "No supported package manager found (apt, dnf, yum, pacman, zypper, apk)."
    warn "Install manually: cmake, make, MinGW-w64 (x86_64-w64-mingw32-*), python3, go"
    exit 1
fi

install_with "$PM" "${PKGS[@]}" || {
    fail "Package installation failed."
    exit 1
}

# Some distros ship the cross tools only after re-login; re-check PATH anyway.
hash -r

if ! report; then
    warn "Some tools are still missing. If a package installed into a non-standard"
    warn "prefix, add it to PATH and re-run '$0 --check'."
    exit 1
fi

if [ "$DO_BUILD" -eq 1 ]; then
    info "Building ../builder"
    ( cd "$SCRIPT_DIR" && go build -trimpath -ldflags="-s -w" -o ../builder . )
    ok "Built $(cd "$SCRIPT_DIR/.." && pwd)/builder"
else
    info "Next: cd $(basename "$SCRIPT_DIR") && go build -trimpath -ldflags=\"-s -w\" -o ../builder ."
fi

info "You can now build a client: ./builder -panel-urls https://your-panel/api/miners/submit -output dist"

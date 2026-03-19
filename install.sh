#!/bin/bash
# bts installer — downloads the latest release binary for your platform
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[OK]${NC} $1"; }
print_error()   { echo -e "${RED}[ERROR]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARN]${NC} $1"; }

detect_platform() {
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    local arch=$(uname -m)

    case $os in
        darwin) OS="darwin" ;;
        linux)  OS="linux" ;;
        *)
            print_error "Unsupported OS: $os (supported: macOS, Linux)"
            exit 1 ;;
    esac

    case $arch in
        x86_64|amd64)  ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *)
            print_error "Unsupported architecture: $arch (supported: x86_64, arm64)"
            exit 1 ;;
    esac

    PLATFORM="${OS}_${ARCH}"
    print_success "Platform: $PLATFORM"
}

get_latest_version() {
    local url="https://api.github.com/repos/jlim/bts/releases/latest"

    if command -v curl &> /dev/null; then
        VERSION=$(curl -s "$url" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')
    elif command -v wget &> /dev/null; then
        VERSION=$(wget -qO- "$url" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')
    else
        print_error "curl or wget required"
        exit 1
    fi

    if [ -z "$VERSION" ]; then
        print_error "Failed to fetch latest version from GitHub"
        echo "  Try: $0 --version 0.1.0"
        echo "  Or:  go install github.com/jlim/bts/cmd/bts@latest"
        exit 1
    fi

    print_success "Latest version: $VERSION"
}

download_and_install() {
    local archive="bts_${VERSION}_${OS}_${ARCH}.tar.gz"
    local base_url="https://github.com/jlim/bts/releases/download/v${VERSION}"
    local tmp=$(mktemp -d)
    trap "rm -rf '$tmp'" EXIT

    print_info "Downloading $archive..."
    if command -v curl &> /dev/null; then
        curl -fsSL "$base_url/$archive" -o "$tmp/$archive" || { print_error "Download failed"; exit 1; }
        curl -fsSL "$base_url/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null || true
    else
        wget -q "$base_url/$archive" -O "$tmp/$archive" || { print_error "Download failed"; exit 1; }
        wget -q "$base_url/checksums.txt" -O "$tmp/checksums.txt" 2>/dev/null || true
    fi

    # Verify checksum
    if [ -f "$tmp/checksums.txt" ]; then
        local expected=$(grep "$archive" "$tmp/checksums.txt" | awk '{print $1}')
        if [ -n "$expected" ]; then
            local actual=""
            if command -v sha256sum &> /dev/null; then
                actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
            elif command -v shasum &> /dev/null; then
                actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
            fi
            if [ -n "$actual" ]; then
                if [ "$expected" != "$actual" ]; then
                    print_error "Checksum mismatch!"
                    exit 1
                fi
                print_success "Checksum verified"
            fi
        fi
    fi

    # Extract
    tar -xzf "$tmp/$archive" -C "$tmp" || { print_error "Extraction failed"; exit 1; }
    chmod +x "$tmp/bts"

    # Determine install directory
    if [ -n "$INSTALL_DIR" ]; then
        TARGET_DIR="$INSTALL_DIR"
    elif command -v go &> /dev/null; then
        local gobin=$(go env GOBIN 2>/dev/null)
        local gopath=$(go env GOPATH 2>/dev/null)
        if [ -n "$gobin" ] && [ -d "$gobin" ]; then
            TARGET_DIR="$gobin"
        elif [ -n "$gopath" ] && [ -d "$gopath/bin" ]; then
            TARGET_DIR="$gopath/bin"
        else
            TARGET_DIR="$HOME/.local/bin"
        fi
    else
        TARGET_DIR="$HOME/.local/bin"
    fi

    mkdir -p "$TARGET_DIR"
    mv "$tmp/bts" "$TARGET_DIR/bts" || { cp "$tmp/bts" "$TARGET_DIR/bts" && chmod +x "$TARGET_DIR/bts"; }
    print_success "Installed to $TARGET_DIR/bts"
}

verify_installation() {
    if command -v bts &> /dev/null; then
        print_success "bts installed successfully!"
        echo ""
        bts --help 2>&1 | head -3
        echo ""
        print_info "Get started:"
        echo "  bts init .       # Initialize in current project"
    else
        print_warning "'bts' not found in PATH"
        print_info "Add to your shell config:"
        echo ""
        echo "  export PATH=\"\$PATH:$TARGET_DIR\""
        echo ""
        print_info "Then restart your shell or run: source ~/.zshrc"
    fi
}

main() {
    echo ""
    echo "  bts installer"
    echo "  ─────────────"
    echo ""

    VERSION=""
    INSTALL_DIR=""

    while [[ $# -gt 0 ]]; do
        case $1 in
            --version)     VERSION="$2"; shift 2 ;;
            --install-dir) INSTALL_DIR="$2"; shift 2 ;;
            -h|--help)
                echo "Usage: $0 [OPTIONS]"
                echo ""
                echo "Options:"
                echo "  --version VERSION    Install specific version (default: latest)"
                echo "  --install-dir DIR    Install to custom directory"
                echo "  -h, --help           Show this help"
                echo ""
                echo "Examples:"
                echo "  curl -fsSL https://raw.githubusercontent.com/jlim/bts/main/install.sh | bash"
                echo "  $0 --version 0.1.0"
                echo "  $0 --install-dir /usr/local/bin"
                exit 0 ;;
            *) print_error "Unknown option: $1"; exit 1 ;;
        esac
    done

    detect_platform

    if [ -z "$VERSION" ]; then
        get_latest_version
    else
        print_info "Version: $VERSION"
    fi

    download_and_install
    verify_installation

    echo ""
    print_success "Done!"
    print_info "https://github.com/jlim/bts"
    echo ""
}

main "$@"

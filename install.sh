#!/bin/bash
# ad-cli Installer
# Builds adg from source, checks prerequisites, and runs 'adg init'

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="$SCRIPT_DIR/bin"

# ── Colors ─────────────────────────────────────────────────────────────────

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    RESET='\033[0m'
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BLUE='\033[0;34m'
    CYAN='\033[0;36m'
else
    RESET='' RED='' GREEN='' YELLOW='' BLUE='' CYAN=''
fi

ok()   { echo -e "${GREEN}✓${RESET}  $*"; }
warn() { echo -e "${YELLOW}⚠${RESET}  $*"; }
err()  { echo -e "${RED}✗${RESET}  $*"; }
info() { echo -e "${CYAN}→${RESET}  $*"; }

# ── Banner ─────────────────────────────────────────────────────────────────

echo ""
echo -e "${BLUE}────────────────────────────────────${RESET}"
echo -e "${BLUE}  adg — Azure DevOps CLI  •  Install${RESET}"
echo -e "${BLUE}────────────────────────────────────${RESET}"
echo ""

# ── Step 1: Go toolchain ───────────────────────────────────────────────────

echo -e "${CYAN}[1/4] Checking Go toolchain...${RESET}"
echo ""

if ! command -v go &> /dev/null; then
    err "'go' not found — Go is required to build adg from source."
    echo ""
    echo "    Download from: https://go.dev/dl/"
    echo "    Or:  winget install GoLang.Go"
    echo ""
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}')
ok "Go found ($GO_VERSION)"
echo ""

# ── Step 2: Build ──────────────────────────────────────────────────────────

echo -e "${CYAN}[2/4] Checking make + building adg...${RESET}"
echo ""

if ! command -v make &> /dev/null; then
    warn "'make' is not installed. It is needed to build adg from source."
    echo ""

    if command -v winget &> /dev/null; then
        read -p "      Install make via winget? (Y/n): " -n 1 -r; echo ""
        if [[ ! $REPLY =~ ^[Nn]$ ]]; then
            info "Installing make via winget..."
            if winget install --id GnuWin32.Make -e --silent; then
                ok "make installed. You may need to restart your shell to pick up the new PATH."
            else
                err "winget install failed."
                echo "      Run in an elevated shell:  winget install GnuWin32.Make"
                exit 1
            fi
        else
            warn "Skipping make installation."
            exit 1
        fi
    elif command -v choco &> /dev/null; then
        read -p "      Install make via Chocolatey? (Y/n): " -n 1 -r; echo ""
        if [[ ! $REPLY =~ ^[Nn]$ ]]; then
            info "Installing make via Chocolatey..."
            if choco install make -y; then
                ok "make installed."
            else
                err "choco install failed."
                echo "      Run in an elevated shell:  choco install make"
                exit 1
            fi
        else
            warn "Skipping make installation."
            exit 1
        fi
    else
        err "Neither winget nor choco found — cannot install make automatically."
        echo "      Install manually from an elevated shell:"
        echo "        winget install GnuWin32.Make"
        echo "        choco install make"
        exit 1
    fi
else
    MAKE_VERSION=$(make --version | head -n 1)
    ok "make found ($MAKE_VERSION)"
fi
echo ""

info "Building adg (make build-local)..."
echo ""

if make -C "$SCRIPT_DIR" build-local; then
    ok "Built adg.exe → $BIN_DIR/adg.exe"
else
    err "Build failed. Check the output above."
    exit 1
fi
echo ""

# ── Step 3: Azure CLI ──────────────────────────────────────────────────────

echo -e "${CYAN}[3/4] Checking Azure CLI...${RESET}"
echo ""

AZ_WAS_INSTALLED=false

if ! command -v az &> /dev/null; then
    warn "Azure CLI (az) is not installed."
    echo "      All adg commands that call Azure DevOps APIs require it."
    echo ""

    if command -v winget &> /dev/null; then
        read -p "      Install via winget? (Y/n): " -n 1 -r; echo ""
        if [[ ! $REPLY =~ ^[Nn]$ ]]; then
            info "Installing Azure CLI via winget..."
            if winget install --id Microsoft.AzureCLI -e --silent; then
                ok "Azure CLI installed."
                AZ_WAS_INSTALLED=true
            else
                err "winget install failed. Install manually:"
                echo "      winget install Microsoft.AzureCLI"
                echo "      or: https://aka.ms/installazurecliwindows"
            fi
        else
            warn "Skipping Azure CLI installation."
        fi
    elif command -v choco &> /dev/null; then
        read -p "      Install via Chocolatey? (Y/n): " -n 1 -r; echo ""
        if [[ ! $REPLY =~ ^[Nn]$ ]]; then
            info "Installing Azure CLI via Chocolatey..."
            if choco install azure-cli -y; then
                ok "Azure CLI installed."
                AZ_WAS_INSTALLED=true
            else
                err "choco install failed. Install manually:"
                echo "      https://aka.ms/installazurecliwindows"
            fi
        else
            warn "Skipping Azure CLI installation."
        fi
    else
        warn "Neither winget nor choco found. Install manually:"
        echo "      https://aka.ms/installazurecliwindows"
        echo "      winget install Microsoft.AzureCLI"
    fi
else
    AZ_VERSION=$(az version --query '"azure-cli"' -o tsv 2>/dev/null || echo "unknown")
    ok "Azure CLI found ($AZ_VERSION)"
fi
echo ""

# ── Step 4: azure-devops extension ────────────────────────────────────────

echo -e "${CYAN}[4/4] Checking azure-devops extension...${RESET}"
echo ""

if command -v az &> /dev/null || [ "$AZ_WAS_INSTALLED" = true ]; then
    if az extension list --query "[?name=='azure-devops'].name" -o tsv 2>/dev/null | grep -q "azure-devops"; then
        ok "azure-devops extension already installed"
    else
        warn "azure-devops extension is not installed."
        echo ""
        read -p "      Install it now? (Y/n): " -n 1 -r; echo ""
        if [[ ! $REPLY =~ ^[Nn]$ ]]; then
            info "Installing azure-devops extension..."
            if az extension add --name azure-devops; then
                ok "azure-devops extension installed."
            else
                err "Failed. Run manually: az extension add --name azure-devops"
            fi
        else
            warn "Skipping. Run when ready: az extension add --name azure-devops"
        fi
    fi
else
    warn "Skipping extension check (Azure CLI not available)."
fi
echo ""

# ── Shell integration ──────────────────────────────────────────────────────

echo -e "${CYAN}Setting up shell integration...${RESET}"
echo ""
"$BIN_DIR/adg.exe" init

# ── Done ───────────────────────────────────────────────────────────────────

echo ""
ok "Installation complete!"
echo ""
echo "    Reload your shell:  source ~/.bashrc"
echo "    Get started:        adg --help"
echo ""

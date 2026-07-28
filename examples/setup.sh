#!/bin/bash
#
# setup.sh — Prepare prerequisite files for Packer Cloud-Hypervisor examples
#
# Downloads Alpine kernel/initramfs, EDK2 firmware, and Ubuntu cloud image;
# creates Alpine rootfs; validates cloud-hypervisor binary; creates TAP device.
# Safe to run multiple times (idempotent).
#
# Usage:
#   ./setup.sh              # Full setup
#   ./setup.sh --dry-run    # Check dependencies, report what would be done
#   ./setup.sh --help       # Show this help
#
# Environment:
#   CH_BINARY_PATH          Override path to cloud-hypervisor binary

set -Eeuo pipefail
shopt -s inherit_errexit
IFS=$'\n\t'

# ---- Script location ----
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
SCRIPT_NAME="$(basename -- "${BASH_SOURCE[0]}")"

# ---- Configuration ----
# Alpine
ALPINE_RELEASE="v3.20"
ALPINE_VERSION="3.20.0"
ALPINE_BASE_URL="https://dl-cdn.alpinelinux.org/alpine/${ALPINE_RELEASE}/releases/x86_64"
ALPINE_KERNEL_URL="${ALPINE_BASE_URL}/netboot/vmlinuz-virt"
ALPINE_INITRAMFS_URL="${ALPINE_BASE_URL}/netboot/initramfs-virt"
ALPINE_MINIROOTFS_URL="${ALPINE_BASE_URL}/alpine-minirootfs-${ALPINE_VERSION}-x86_64.tar.gz"

# Asset filenames
ALPINE_KERNEL_DEST="alpine-vmlinuz"
ALPINE_INITRAMFS_DEST="alpine-initramfs"
ALPINE_ROOTFS_FILE="alpine-rootfs.raw"
ALPINE_ROOTFS_SIZE="2G"
EDK2_DEST="CLOUDHV.fd"
UBUNTU_QCOW2_DEST="noble-server-cloudimg-amd64.qcow2"
UBUNTU_RAW_DEST="noble-server-cloudimg-amd64.raw"

# URLs
EDK2_URL="https://github.com/cloud-hypervisor/edk2/releases/download/ch-1e1b96f126/CLOUDHV.fd"
UBUNTU_CLOUD_IMAGE_URL="https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"

# Network
TAP_DEVICE="ch-tap-0"

# ---- Global state ----
DRY_RUN=false
DOWNLOAD_CMD=""
SUDO_CMD=""
declare -a RESULTS=()

# ---- Color support ----
if [[ -t 1 ]]; then
    C_RESET='\033[0m'
    C_GREEN='\033[0;32m'
    C_RED='\033[0;31m'
    C_YELLOW='\033[0;33m'
    C_BLUE='\033[0;34m'
    C_BOLD='\033[1m'
else
    C_RESET=''
    C_GREEN=''
    C_RED=''
    C_YELLOW=''
    C_BLUE=''
    C_BOLD=''
fi

# ---------------------------------------------------------------------------
# Logging helpers
# ---------------------------------------------------------------------------
log_info()  { printf '%b[INFO]%b  %s\n' "${C_BLUE}" "${C_RESET}" "$*" >&2; }
log_pass()  { printf '%b[PASS]%b  %s\n' "${C_GREEN}" "${C_RESET}" "$*" >&2; }
log_fail()  { printf '%b[FAIL]%b  %s\n' "${C_RED}" "${C_RESET}" "$*" >&2; }
log_skip()  { printf '%b[SKIP]%b %s\n' "${C_YELLOW}" "${C_RESET}" "$*" >&2; }
log_error() { printf '%b[ERROR]%b %s\n' "${C_RED}" "${C_RESET}" "$*" >&2; }
log_step()  { printf '\n%b=== %s ===%b\n' "${C_BOLD}" "$*" "${C_RESET}" >&2; }
log_dry_run(){ printf '    %b[DRY-RUN]%b Would execute: %s\n' "${C_YELLOW}" "${C_RESET}" "$*" >&2; }

# ---------------------------------------------------------------------------
# Result tracking
# ---------------------------------------------------------------------------
record_result() {
    local step="$1"
    local rc="$2"
    case "$rc" in
        0) RESULTS+=("PASS|${step}");    log_pass "${step}" ;;
        2) RESULTS+=("SKIP|${step}");    log_skip "${step}" ;;
        *) RESULTS+=("FAIL|${step}");    log_fail "${step}" ;;
    esac
    # Return 0 regardless — rc is tracked in RESULTS array for summary.
    # Non-zero return with set -e would abort the script on SKIP.
    return 0
}

print_summary() {
    local pass=0 skip=0 fail=0
    local line status step

    printf '\n'
    printf '%b\n' "${C_BOLD}═══════════════════════════════════════════${C_RESET}"
    printf '%b  %b\n' "${C_BOLD}" "Setup Summary${C_RESET}"
    printf '%b\n' "${C_BOLD}═══════════════════════════════════════════${C_RESET}"

    for line in "${RESULTS[@]}"; do
        status="${line%%|*}"
        step="${line#*|}"
        case "${status}" in
            PASS) printf '  %b✔%b %s\n' "${C_GREEN}" "${C_RESET}" "${step}"; ((++pass)) ;;
            SKIP) printf '  %b➜%b %s (already exists)\n' "${C_YELLOW}" "${C_RESET}" "${step}"; ((++skip)) ;;
            FAIL) printf '  %b✘%b %s\n' "${C_RED}" "${C_RESET}" "${step}"; ((++fail)) ;;
        esac
    done

    printf '%b\n' "${C_BOLD}───────────────────────────────────────────${C_RESET}"
    printf '  Total: %d  %bPASS: %d%b  %bSKIP: %d%b  %bFAIL: %d%b\n' \
        "$((pass + skip + fail))" \
        "${C_GREEN}" "$pass" "${C_RESET}" \
        "${C_YELLOW}" "$skip" "${C_RESET}" \
        "${C_RED}" "$fail" "${C_RESET}"
    printf '%b\n' "${C_BOLD}───────────────────────────────────────────${C_RESET}"

    if (( fail > 0 )); then
        printf '  %bVerdict: NOT_READY — resolve failures above and re-run%b\n' "${C_RED}" "${C_RESET}"
    else
        printf '  %bVerdict: READY — all prerequisites are in place%b\n' "${C_GREEN}" "${C_RESET}"
    fi
    printf '%b\n' "${C_BOLD}═══════════════════════════════════════════${C_RESET}"
    printf '\n'
}

# ---------------------------------------------------------------------------
# Execution helpers
# ---------------------------------------------------------------------------
run_cmd() {
    if [[ "$DRY_RUN" == "true" ]]; then
        local IFS=' '
        log_dry_run "$*"
        return 0
    fi
    "$@"
}

run_sudo() {
    if [[ "$DRY_RUN" == "true" ]]; then
        local IFS=' '
        log_dry_run "$*"
        return 0
    fi
    if [[ -n "$SUDO_CMD" ]]; then
        "$SUDO_CMD" "$@"
    else
        "$@"
    fi
}

download_file() {
    local url="$1"
    local dest="$2"
    local desc="$3"

    if [[ -f "$dest" && -s "$dest" ]]; then
        return 2
    fi

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY-RUN] Would download ${desc} → $(basename "${dest}")"
        return 0
    fi

    log_info "Downloading ${desc}..."
    if [[ "$DOWNLOAD_CMD" == "curl" ]]; then
        curl -fsSL -o "$dest" "$url"
    elif [[ "$DOWNLOAD_CMD" == "wget" ]]; then
        wget -q -O "$dest" "$url"
    else
        log_error "No download tool available"
        return 1
    fi
}

# ---------------------------------------------------------------------------
# Step: Parse arguments
# ---------------------------------------------------------------------------
parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            --help|-h)
                usage 0
                ;;
            *)
                printf '%b[ERROR]%b Unknown option: %s\n' "${C_RED}" "${C_RESET}" "$1" >&2
                usage 1
                ;;
        esac
    done
}

usage() {
    cat <<EOF
Usage: ${SCRIPT_NAME} [OPTIONS]

Prepare prerequisite files for Packer Cloud-Hypervisor examples.

Options:
  --dry-run   Check dependencies and report actions without making changes
  --help      Show this help and exit

Environment:
  CH_BINARY_PATH  Path to cloud-hypervisor binary (overrides PATH lookup)

Examples:
  ${SCRIPT_NAME}
  ${SCRIPT_NAME} --dry-run
EOF
    exit "${1:-0}"
}

# ---------------------------------------------------------------------------
# Step: Check dependencies
# ---------------------------------------------------------------------------
check_dependencies() {
    local missing=0
    local -a required_tools=(
        "qemu-img:qemu-img (from qemu-utils or qemu-img package)"
        "ip:ip (from iproute2 package)"
        "truncate:truncate (from coreutils package)"
        "mkfs.ext4:mkfs.ext4 (from e2fsprogs package)"
        "mount:mount (from util-linux package)"
        "chroot:chroot (from coreutils package)"
        "parted:parted (from parted package)"
        "losetup:losetup (from util-linux package)"
    )

    for entry in "${required_tools[@]}"; do
        local cmd="${entry%%:*}"
        local hint="${entry#*:}"
        if ! command -v "$cmd" &>/dev/null; then
            log_error "Required dependency not found: ${cmd}"
            printf "    %s\n" "${hint}" >&2
            ((++missing))
        fi
    done

    # sudo — only required if not root
    if [[ $EUID -ne 0 ]]; then
        if ! command -v sudo &>/dev/null; then
            log_error "Required dependency not found: sudo"
            printf "    Install sudo or run as root\n" >&2
            ((++missing))
        fi
    fi

    # Download tool: curl or wget
    if command -v curl &>/dev/null; then
        DOWNLOAD_CMD="curl"
    elif command -v wget &>/dev/null; then
        DOWNLOAD_CMD="wget"
    else
        log_error "No download tool found: install curl or wget"
        ((++missing))
    fi

    if (( missing > 0 )); then
        if (( missing == 1 )); then
            log_error "1 required dependency is missing — install before re-running"
        else
            log_error "${missing} required dependencies are missing — install before re-running"
        fi
        return 1
    fi

    log_pass "All required dependencies found"
    return 0
}

# ---------------------------------------------------------------------------
# Step: Create assets directory
# ---------------------------------------------------------------------------
create_assets_dir() {
    if [[ -d "$ASSETS_DIR" ]]; then
        return 2
    fi
    run_cmd mkdir -p "$ASSETS_DIR"
}

# ---- Determine dynamic paths ----
ASSETS_DIR="${SCRIPT_DIR}/assets"

# ---------------------------------------------------------------------------
# Step: Download Alpine minirootfs (used in rootfs creation)
# ---------------------------------------------------------------------------
download_alpine_minirootfs() {
    local dest="${ASSETS_DIR}/alpine-minirootfs-${ALPINE_VERSION}-x86_64.tar.gz"
    download_file "$ALPINE_MINIROOTFS_URL" "$dest" \
        "Alpine ${ALPINE_VERSION} minirootfs"
}

# ---------------------------------------------------------------------------
# Step: Create Alpine rootfs with full sys installation
# ---------------------------------------------------------------------------
create_alpine_rootfs() {
    local rootfs_path="${ASSETS_DIR}/${ALPINE_ROOTFS_FILE}"
    local minirootfs_path="${ASSETS_DIR}/alpine-minirootfs-${ALPINE_VERSION}-x86_64.tar.gz"
    local mnt_dir="${SCRIPT_DIR}/_mnt_alpine"
    local vmlinuz_dest="${ASSETS_DIR}/${ALPINE_KERNEL_DEST}"
    local initramfs_dest="${ASSETS_DIR}/${ALPINE_INITRAMFS_DEST}"

    # Skip if everything already exists
    if [[ -f "$rootfs_path" && -s "$rootfs_path" && -f "$vmlinuz_dest" && -f "$initramfs_dest" ]]; then
        return 2
    fi

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY-RUN] Would create Alpine sys rootfs: ${ALPINE_ROOTFS_FILE} (${ALPINE_ROOTFS_SIZE}) with kernel + initramfs"
        return 0
    fi

    # Download minirootfs first if not present
    if [[ ! -f "$minirootfs_path" ]]; then
        log_info "Downloading Alpine minirootfs ${ALPINE_VERSION}..."
        "$DOWNLOAD_CMD" -fsSL -o "$minirootfs_path" "$ALPINE_MINIROOTFS_URL"
    fi

    log_info "Creating Alpine sys rootfs: ${ALPINE_ROOTFS_FILE} (${ALPINE_ROOTFS_SIZE})..."

    # Create disk image with MBR partition table
    truncate -s "${ALPINE_ROOTFS_SIZE}" "$rootfs_path"
    parted -s "$rootfs_path" mklabel msdos
    parted -s "$rootfs_path" mkpart primary ext4 1M 100%
    parted -s "$rootfs_path" set 1 boot on

    # Setup loop device with partition scanning
    local loop_dev
    loop_dev=$(losetup -f --show -P "$rootfs_path")
    local part_dev="${loop_dev}p1"

    # Format root partition with Alpine label
    mkfs.ext4 -q -F -L "alpine_root" "$part_dev"

    # Mount partition
    mkdir -p "$mnt_dir"
    mount "$part_dev" "$mnt_dir"

    # Extract minirootfs
    log_info "Extracting Alpine minirootfs..."
    tar xzf "$minirootfs_path" -C "$mnt_dir"

    # Prepare chroot environment
    mount --bind /dev "$mnt_dir/dev"
    mount --bind /proc "$mnt_dir/proc"
    mount --bind /sys "$mnt_dir/sys"

    # Write resolv.conf and fstab
    echo 'nameserver 1.1.1.1' > "$mnt_dir/etc/resolv.conf"
    echo 'nameserver 8.8.8.8' >> "$mnt_dir/etc/resolv.conf"

    cat > "$mnt_dir/etc/fstab" << 'FSTAB'
/dev/vda1  /  ext4  rw,relatime  0 1
FSTAB

    cat > "$mnt_dir/etc/network/interfaces" << 'NETWORK'
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet dhcp
NETWORK

    # Install packages and configure system via chroot
    chroot "$mnt_dir" /bin/sh << 'CHROOT_EOF'
export PATH="/usr/sbin:/usr/bin:/sbin:/bin"

# Set up APK repositories
cat > /etc/apk/repositories << 'REPOS'
https://dl-cdn.alpinelinux.org/alpine/v3.20/main
https://dl-cdn.alpinelinux.org/alpine/v3.20/community
REPOS

# Install base system, kernel, and SSH
apk update --quiet
apk add --quiet alpine-base linux-virt openssh mkinitfs

# Set root password
echo "root:root" | chpasswd

# Configure SSH
sed -i 's/^#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config
sed -i 's/^#PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config

# Enable SSH and networking services
rc-update add sshd default
rc-update add networking boot

# Enable serial console on ttyS0
alpine_conf_path="/etc/inittab"
if grep -q '^#ttyS0' "$alpine_conf_path"; then
    sed -i 's|^#ttyS0.*|ttyS0::respawn:/sbin/getty -L ttyS0 115200 vt100|' "$alpine_conf_path"
fi

# Ensure ttyS0 is configured in securetty
echo "ttyS0" >> /etc/securetty 2>/dev/null || true

# Generate initramfs for the installed kernel
KVER=$(ls /lib/modules/ 2>/dev/null | head -1)
if [ -n "$KVER" ]; then
    mkinitfs -o /boot/initramfs-virt "$KVER"
fi

# Write builder metadata
echo "Alpine VM provisioned by Packer Cloud-Hypervisor" > /etc/builder-info
CHROOT_EOF

    # Copy generated kernel and initramfs to assets
    if [[ -f "$mnt_dir/boot/vmlinuz-virt" ]]; then
        cp "$mnt_dir/boot/vmlinuz-virt" "$vmlinuz_dest"
        log_pass "Copied kernel: ${ALPINE_KERNEL_DEST}"
    else
        log_error "Generated kernel not found in boot directory"
    fi

    if [[ -f "$mnt_dir/boot/initramfs-virt" ]]; then
        cp "$mnt_dir/boot/initramfs-virt" "$initramfs_dest"
        log_pass "Copied initramfs: ${ALPINE_INITRAMFS_DEST}"
    else
        log_error "Generated initramfs not found in boot directory"
    fi

    # Cleanup
    umount -R "$mnt_dir" 2>/dev/null || true
    rmdir "$mnt_dir" 2>/dev/null || true
    losetup -d "$loop_dev" 2>/dev/null || true

    log_pass "Created Alpine sys rootfs: ${ALPINE_ROOTFS_FILE}"
}

# ---------------------------------------------------------------------------
# Step: Download EDK2 firmware
# ---------------------------------------------------------------------------
download_edk2() {
    download_file "$EDK2_URL" "${ASSETS_DIR}/${EDK2_DEST}" \
        "EDK2 Cloud Hypervisor firmware"
}

# ---------------------------------------------------------------------------
# Step: Download Ubuntu cloud image
# ---------------------------------------------------------------------------
download_ubuntu_cloud_image() {
    download_file "$UBUNTU_CLOUD_IMAGE_URL" "${ASSETS_DIR}/${UBUNTU_QCOW2_DEST}" \
        "Ubuntu Noble cloud image"
}

# ---------------------------------------------------------------------------
# Step: Convert Ubuntu cloud image to raw
# ---------------------------------------------------------------------------
convert_ubuntu_to_raw() {
    local qcow2_path="${ASSETS_DIR}/${UBUNTU_QCOW2_DEST}"
    local raw_path="${ASSETS_DIR}/${UBUNTU_RAW_DEST}"

    if [[ -f "$raw_path" && -s "$raw_path" ]]; then
        return 2
    fi

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY-RUN] Would convert ${UBUNTU_QCOW2_DEST} → ${UBUNTU_RAW_DEST}"
        return 0
    fi

    if [[ ! -f "$qcow2_path" ]]; then
        log_error "Ubuntu qcow2 image not found at ${UBUNTU_QCOW2_DEST} — download it first"
        return 1
    fi

    log_info "Converting ${UBUNTU_QCOW2_DEST} → ${UBUNTU_RAW_DEST}..."
    qemu-img convert -O raw "$qcow2_path" "$raw_path"
    log_pass "Converted Ubuntu cloud image to raw format"
}

# ---------------------------------------------------------------------------
# Step: Check cloud-hypervisor binary
# ---------------------------------------------------------------------------
check_cloud_hypervisor() {
    local ch_binary=""

    if [[ -n "${CH_BINARY_PATH:-}" ]]; then
        ch_binary="$CH_BINARY_PATH"
        if [[ ! -x "$ch_binary" ]]; then
            log_error "CH_BINARY_PATH points to non-executable: ${ch_binary}"
            return 1
        fi
    elif command -v cloud-hypervisor &>/dev/null; then
        ch_binary="$(command -v cloud-hypervisor)"
    else
        log_error "cloud-hypervisor not found on PATH"
        printf "    Install cloud-hypervisor or set CH_BINARY_PATH\n" >&2
        return 1
    fi

    log_pass "cloud-hypervisor found: ${ch_binary}"
    return 0
}

# ---------------------------------------------------------------------------
# Step: Create TAP device
# ---------------------------------------------------------------------------
create_tap_device() {
    if ip link show "${TAP_DEVICE}" &>/dev/null; then
        return 2
    fi

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY-RUN] Would create TAP device: ${TAP_DEVICE}"
        return 0
    fi

    log_info "Creating TAP device: ${TAP_DEVICE}..."
    run_sudo ip tuntap add dev "${TAP_DEVICE}" mode tap
    log_pass "Created TAP device: ${TAP_DEVICE}"
}

# ---------------------------------------------------------------------------
# Main orchestration
# ---------------------------------------------------------------------------
main() {
    # --- Parse arguments first (may exit for --help) ---
    parse_args "$@"

    printf '%b\n' "${C_BOLD}╔══════════════════════════════════════════╗${C_RESET}"
    printf '%b\n' "${C_BOLD}║  Cloud-Hypervisor Packer Prerequisites   ║${C_RESET}"
    printf '%b\n' "${C_BOLD}╚══════════════════════════════════════════╝${C_RESET}"
    if [[ "$DRY_RUN" == "true" ]]; then
        printf '  %bDry-run mode — no changes will be made%b\n\n' "${C_YELLOW}" "${C_RESET}"
    else
        printf '\n'
    fi

    local rc=0

    # --- Determine sudo ---
    if [[ $EUID -eq 0 ]]; then
        SUDO_CMD=""
    elif command -v sudo &>/dev/null; then
        SUDO_CMD="sudo"
    fi

    # --- 1. Check dependencies ---
    log_step "Dependencies"
    rc=0
    check_dependencies || rc=$?
    record_result "Dependency checks" "$rc"
    if [[ "$rc" -eq 1 ]]; then
        print_summary
        exit 1
    fi

    # --- 2. Create assets directory ---
    log_step "Assets directory"
    rc=0
    create_assets_dir || rc=$?
    record_result "Create assets/" "$rc"

    # --- 3. Download Alpine minirootfs ---
    log_step "Alpine minirootfs"
    rc=0
    download_alpine_minirootfs || rc=$?
    record_result "Download Alpine minirootfs" "$rc"

    # --- 4. Create Alpine rootfs with kernel + initramfs ---
    log_step "Alpine rootfs"
    rc=0
    create_alpine_rootfs || rc=$?
    record_result "Create Alpine rootfs" "$rc"

    # --- 6. Download EDK2 firmware ---
    log_step "EDK2 firmware"
    rc=0
    download_edk2 || rc=$?
    record_result "Download EDK2 firmware" "$rc"

    # --- 7. Download Ubuntu cloud image ---
    log_step "Ubuntu cloud image"
    rc=0
    download_ubuntu_cloud_image || rc=$?
    record_result "Download Ubuntu cloud image" "$rc"

    # --- 8. Convert Ubuntu cloud image to raw ---
    log_step "Ubuntu raw image"
    rc=0
    convert_ubuntu_to_raw || rc=$?
    record_result "Convert Ubuntu to raw" "$rc"

    # --- 9. Check cloud-hypervisor binary ---
    log_step "cloud-hypervisor"
    rc=0
    check_cloud_hypervisor || rc=$?
    record_result "cloud-hypervisor binary" "$rc"

    # --- 10. Create TAP device ---
    log_step "TAP device"
    rc=0
    create_tap_device || rc=$?
    record_result "Create TAP device" "$rc"

    # --- Summary ---
    print_summary

    # Exit with 0 only if no failures
    for line in "${RESULTS[@]}"; do
        if [[ "${line%%|*}" == "FAIL" ]]; then
            exit 1
        fi
    done
    exit 0
}

main "$@"

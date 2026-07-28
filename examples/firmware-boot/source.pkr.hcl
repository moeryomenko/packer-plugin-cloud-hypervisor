// source.pkr.hcl -- Defines the cloud-hypervisor builder source for
// firmware/disk-boot mode. This source boots a VM using UEFI firmware
// (EDK2) with an Ubuntu cloud image as the root disk.
//
// File paths use path.root which resolves relative to this template
// directory. Assets are in examples/assets/ (one level up).

source "cloud-hypervisor" "firmware-boot" {
  // Cloud-Hypervisor binary path (default: "cloud-hypervisor" on PATH)
  ch_binary_path = var.ch_binary

  // CPU and memory allocation
  vcpus  = var.vcpus
  memory = var.memory

  // Boot payload: firmware (UEFI/EDK2) instead of a direct kernel.
  // This enables booting standard cloud images that rely on UEFI.
  firmware = "${path.root}/../assets/CLOUDHV.fd"

  // Disk images to attach. The Ubuntu cloud image is writable (readonly =
  // false) so that Packer can capture it as an artifact with changes.
  disk_images {
    path       = "${path.root}/../assets/noble-server-cloudimg-amd64.raw"
    readonly   = false
    image_type = "raw"
  }

  // Network interface attached to the pre-configured TAP device.
  // Required for SSH connectivity to the VM.
  network_interfaces {
    tap = var.tap_device
  }

  // No communicator (headless build) -- disk image is captured as artifact.
  // For SSH provisioning, configure networking (bridge + DHCP or static IP)
  // and switch communicator to "ssh".
  communicator = "none"

  // Serial and console are set to null since no provisioning is performed
  serial  = "null"
  console = "null"
}

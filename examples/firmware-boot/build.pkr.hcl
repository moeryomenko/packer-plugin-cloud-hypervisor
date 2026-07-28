// build.pkr.hcl -- Orchestrates the firmware-boot build. With communicator =
// "none", the build creates the VM, boots it via UEFI firmware, shuts it
// down, and collects the modified disk image as an artifact.

build {
  name    = "firmware-boot"
  sources = ["source.cloud-hypervisor.firmware-boot"]
}

# build.pkr.hcl -- Orchestrates the kernel-boot build. With communicator =
# "none", the build creates the VM, boots it, shuts it down, and collects
# the modified disk image as an artifact.

build {
  name    = "kernel-boot"
  sources = ["source.cloud-hypervisor.kernel-boot"]
}

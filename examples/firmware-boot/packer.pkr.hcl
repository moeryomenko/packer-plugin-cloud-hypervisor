// packer.pkr.hcl -- Plugin requirements and Packer version constraint for the
// cloud-hypervisor firmware/disk-boot example. The cloud-hypervisor builder
// plugin must be installed before running.

packer {
  required_version = ">= 1.9"

  required_plugins {
    cloud-hypervisor = {
      version = ">= 0.0.1"
      source  = "github.com/moeryomenko/cloud-hypervisor"
    }
  }
}

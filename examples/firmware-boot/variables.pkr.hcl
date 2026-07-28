// variables.pkr.hcl -- Declares user-configurable variables for the
// firmware/disk-boot example.

// Name of the pre-configured TAP device for VM networking. Created by
// setup.sh with `ip tuntap add dev ch-tap-0 mode tap`.
variable "tap_device" {
  type        = string
  default     = "ch-tap-0"
  description = "TAP device name for VM networking"
}

// Path to the cloud-hypervisor binary. Default assumes it is on PATH.
variable "ch_binary" {
  type        = string
  default     = "cloud-hypervisor"
  description = "Path to the cloud-hypervisor binary"
}

// Number of virtual CPUs to allocate to the VM.
variable "vcpus" {
  type        = number
  default     = 2
  description = "Number of virtual CPUs"
}

// Memory in MiB to allocate to the VM. 2048 MiB is recommended for
// Ubuntu to ensure cloud-init and package installs complete reliably.
variable "memory" {
  type        = number
  default     = 2048
  description = "Memory size in MiB"
}

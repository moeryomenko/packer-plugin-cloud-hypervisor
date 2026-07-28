# variables.pkr.hcl -- Declares configurable variables for the kernel-boot
# example. Set via -var, -var-file, or PKR_VAR_<name> environment variables.

# TAP network device created by setup.sh
variable "tap_device" {
  type        = string
  default     = "ch-tap-0"
  description = "TAP device name for VM networking"
}

# Path or name of the cloud-hypervisor binary
variable "ch_binary" {
  type        = string
  default     = "cloud-hypervisor"
  description = "Path or name of the cloud-hypervisor binary"
}

# SSH user -- Ubuntu cloud images use ubuntu by default
variable "ssh_username" {
  type        = string
  default     = "ubuntu"
  description = "SSH username for guest access"
}

# SSH password for the ubuntu user
variable "ssh_password" {
  type        = string
  default     = "ubuntu"
  description = "SSH password for guest access"
}

# Number of vCPUs to assign to the VM
variable "vcpus" {
  type        = number
  default     = 2
  description = "Number of vCPUs for the VM"
}

# Memory in MiB
variable "memory" {
  type        = number
  default     = 2048
  description = "Memory size in MiB"
}

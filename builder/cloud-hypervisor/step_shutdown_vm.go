package cloudhypervisor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/moeryomenko/packer-plugin-cloud-hypervisor/builder/cloud-hypervisor/chclient"
)

const (
	shutdownPollInterval = 2 * time.Second
	shutdownTimeout      = 30 * time.Second
)

// StepShutdownVM sends a graceful shutdown request to the Cloud-Hypervisor VM
// and waits for it to stop by polling VMInfo until the VM is no longer found
// (HTTP 404). If the VM does not stop within the timeout, it force-deletes the
// VM.
type StepShutdownVM struct{}

// Run sends the shutdown request and polls vm.info until the VM is gone. If
// the initial shutdown request fails the error is logged but execution
// continues. If the VM does not stop within shutdownTimeout it is force-
// deleted.
func (s *StepShutdownVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui, ok := state.Get("ui").(packersdk.Ui)
	if !ok {
		err := errors.New("failed to get ui from state bag")
		state.Put("error", err)
		return multistep.ActionHalt
	}
	client, ok := state.Get("ch_client").(*chclient.Client)
	if !ok {
		err := errors.New("failed to get ch_client from state bag")
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	ui.Say("Shutting down Cloud-Hypervisor VM...")

	// REQ-014: Graceful shutdown
	err := client.ShutdownVM(ctx)
	if err != nil {
		// REQ-014 item 2: log and continue
		ui.Error(fmt.Sprintf("Error shutting down VM (continuing): %s", err))
	}

	// Wait for VM to stop (poll vm.info until 404)
	ui.Say("Waiting for VM to shut down...")
	deadline := time.Now().Add(shutdownTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return multistep.ActionHalt
		default:
		}

		_, err := client.VMInfo(ctx)
		if err != nil {
			// VM is gone (expected on 404)
			return multistep.ActionContinue
		}
		time.Sleep(shutdownPollInterval)
	}

	// REQ-014 item 4: force delete on timeout
	ui.Say("Shutdown timeout reached, force deleting VM...")
	if err := client.DeleteVM(ctx); err != nil {
		ui.Error(fmt.Sprintf("Error force deleting VM: %s", err))
	}
	return multistep.ActionContinue
}

// Cleanup forcefully removes the VM. It uses state.GetOk to avoid panicking
// when ui is not in the state bag.
func (s *StepShutdownVM) Cleanup(state multistep.StateBag) {
	client, ok := state.GetOk("ch_client")
	if !ok {
		return
	}
	cl, ok := client.(*chclient.Client)
	if !ok {
		return
	}
	if err := cl.DeleteVM(context.Background()); err != nil {
		// Log only — cleanup errors are not fatal
		log.Printf("[DEBUG] Error deleting VM during cleanup: %s", err)
	}
}

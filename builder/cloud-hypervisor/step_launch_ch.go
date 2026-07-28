package cloudhypervisor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/moeryomenko/packer-plugin-cloud-hypervisor/builder/cloud-hypervisor/chclient"
)

const (
	chPingRetries   = 50 // 50 * 100ms = 5s total
	chPingInterval  = 100 * time.Millisecond
	stderrBufSize   = 4096
	killWaitSeconds = 5
)

// StepLaunchCh launches cloud-hypervisor as a subprocess, waits for the API to
// become ready via ping, and stores the chclient.Client in the state bag.
type StepLaunchCh struct {
	ChBinaryPath string
	ChSocketPath string
}

// Run launches the cloud-hypervisor subprocess and waits for API readiness.
func (s *StepLaunchCh) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui, ok := state.Get("ui").(packersdk.Ui)
	if !ok {
		err := errors.New("failed to get ui from state bag")
		state.Put("error", err)
		return multistep.ActionHalt
	}

	ui.Say("Launching Cloud-Hypervisor...")

	// ChBinaryPath is a user-configured path, not user-supplied input.
	cmd := exec.CommandContext(ctx, s.ChBinaryPath, "--api-socket", s.ChSocketPath) //nolint:gosec

	// Capture stderr for diagnostics
	stderr, err := cmd.StderrPipe()
	if err != nil {
		err := fmt.Errorf("error creating stderr pipe: %w", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	if err := cmd.Start(); err != nil {
		err := fmt.Errorf("error starting Cloud-Hypervisor: %w", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// Store the cmd in state bag for cleanup
	state.Put("ch_cmd", cmd)

	// Wait for API readiness via ping
	ui.Say("Waiting for Cloud-Hypervisor API...")
	client := chclient.New(s.ChSocketPath)

	var pingErr error
	for i := 0; i < chPingRetries; i++ {
		select {
		case <-ctx.Done():
			return multistep.ActionHalt
		default:
		}

		pingErr = client.Ping(ctx)
		if pingErr == nil {
			break
		}
		time.Sleep(chPingInterval)
	}

	if pingErr != nil {
		err := fmt.Errorf("Cloud-Hypervisor did not become ready: %w", pingErr)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// Store client in state bag
	state.Put("ch_client", client)
	ui.Say("Cloud-Hypervisor is ready")

	// Log stderr in background (don't block on it)
	go func() {
		buf := make([]byte, stderrBufSize)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				log.Printf("[DEBUG] Cloud-Hypervisor stderr: %s", string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
	}()

	return multistep.ActionContinue
}

// Cleanup kills the CH subprocess and removes the Unix socket file.
func (s *StepLaunchCh) Cleanup(state multistep.StateBag) {
	// Kill the CH subprocess
	if raw, ok := state.GetOk("ch_cmd"); ok {
		if cmd, ok := raw.(*exec.Cmd); ok && cmd.Process != nil {
			log.Printf("[DEBUG] Terminating Cloud-Hypervisor (pid %d)", cmd.Process.Pid)
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				log.Printf("[DEBUG] SIGTERM failed: %s, sending SIGKILL", err)
				_ = cmd.Process.Kill() //nolint:errcheck
			}
			// Give it a moment to exit
			done := make(chan struct{})
			go func() {
				_ = cmd.Wait() //nolint:errcheck
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(time.Duration(killWaitSeconds) * time.Second):
				log.Printf("[DEBUG] Cloud-Hypervisor did not exit in time, sending SIGKILL")
				_ = cmd.Process.Kill() //nolint:errcheck
			}
		}
	}

	// Remove socket file
	if err := os.Remove(s.ChSocketPath); err != nil && !os.IsNotExist(err) {
		log.Printf("[DEBUG] Error removing socket %s: %s", s.ChSocketPath, err)
	}
}

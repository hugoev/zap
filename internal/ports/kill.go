package ports

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func KillProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}

	// Try graceful termination first (SIGTERM)
	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		// Process might already be gone
		return nil
	}

	// Wait a moment to see if it terminates
	// In a real implementation, you might want to wait and check
	// For now, we'll just send the signal

	return nil
}

func KillProcessForce(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}

	// Force kill (SIGKILL)
	err = process.Signal(syscall.SIGKILL)
	if err != nil {
		// Process might already be gone
		return nil
	}

	return nil
}

func KillProcesses(pids []int) error {
	for _, pid := range pids {
		if err := KillProcess(pid); err != nil {
			return err
		}
	}
	return nil
}

func IsProcessRunning(pid int) bool {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid))
	err := cmd.Run()
	return err == nil
}


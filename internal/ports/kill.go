package ports

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// GracefulTerminationTimeout is how long we wait for SIGTERM to work
	GracefulTerminationTimeout = 3 * time.Second
	// ProcessCheckInterval is how often we check if process is still running
	ProcessCheckInterval = 100 * time.Millisecond
)

func KillProcess(pid int) error {
	// First verify the process exists and is running
	if !IsProcessRunning(pid) {
		return fmt.Errorf("process %d is not running", pid)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}

	// Try graceful termination first (SIGTERM)
	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		// Process might already be gone, verify
		if !IsProcessRunning(pid) {
			return nil // Process already terminated
		}
		return fmt.Errorf("failed to send SIGTERM to process %d: %w", pid, err)
	}

	// Wait for graceful termination with timeout
	deadline := time.Now().Add(GracefulTerminationTimeout)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(pid) {
			return nil // Process terminated gracefully
		}
		time.Sleep(ProcessCheckInterval)
	}

	// If still running after graceful timeout, force kill
	if IsProcessRunning(pid) {
		return KillProcessForce(pid)
	}

	return nil
}

func KillProcessForce(pid int) error {
	// Verify process is still running before attempting kill
	if !IsProcessRunning(pid) {
		return nil // Already terminated
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}

	// Force kill (SIGKILL)
	err = process.Signal(syscall.SIGKILL)
	if err != nil {
		// Check if process is already gone
		if !IsProcessRunning(pid) {
			return nil // Process terminated
		}
		return fmt.Errorf("failed to send SIGKILL to process %d: %w", pid, err)
	}

	// Wait a moment and verify it's actually killed
	time.Sleep(200 * time.Millisecond)
	if IsProcessRunning(pid) {
		return fmt.Errorf("process %d did not terminate after SIGKILL", pid)
	}

	return nil
}

func KillProcesses(pids []int) error {
	var errors []error
	for _, pid := range pids {
		if err := KillProcess(pid); err != nil {
			errors = append(errors, fmt.Errorf("PID %d: %w", pid, err))
			// Continue with other processes even if one fails
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to kill %d of %d processes: %v", len(errors), len(pids), errors)
	}
	return nil
}

func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Use ps to check if process exists
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "pid=")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// If output contains the PID, process is running
	return strings.TrimSpace(string(output)) == strconv.Itoa(pid)
}

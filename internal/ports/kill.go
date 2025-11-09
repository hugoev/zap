package ports

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
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

	// Try to kill process group first (handles child processes)
	if err := KillProcessGroup(pid); err == nil {
		return nil // Successfully killed process group
	}

	// Fallback to single process if process group kill fails
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

// KillProcessGroup kills the entire process group, including child processes
func KillProcessGroup(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID: %d", pid)
	}

	// Get process group ID
	var pgid int
	var err error

	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		// Use unix.Getpgid for Unix systems
		pgid, err = unix.Getpgid(pid)
		if err != nil {
			// If we can't get PGID, fall back to single process
			return fmt.Errorf("failed to get process group: %w", err)
		}
	} else {
		// Fallback for other systems
		return fmt.Errorf("process groups not supported on this platform")
	}

	// Send SIGTERM to entire process group (negative PID means process group)
	err = unix.Kill(-pgid, syscall.SIGTERM)
	if err != nil {
		// If process group doesn't exist, try single process
		if err == unix.ESRCH {
			return fmt.Errorf("process group not found")
		}
		return fmt.Errorf("failed to signal process group: %w", err)
	}

	// Wait for graceful termination
	deadline := time.Now().Add(GracefulTerminationTimeout)
	for time.Now().Before(deadline) {
		if !isProcessGroupRunning(pgid) {
			return nil // Process group terminated gracefully
		}
		time.Sleep(ProcessCheckInterval)
	}

	// Force kill entire group if still running
	if isProcessGroupRunning(pgid) {
		err = unix.Kill(-pgid, syscall.SIGKILL)
		if err != nil && err != unix.ESRCH {
			return fmt.Errorf("failed to force kill process group: %w", err)
		}
		time.Sleep(200 * time.Millisecond)
		if isProcessGroupRunning(pgid) {
			return fmt.Errorf("process group %d did not terminate after SIGKILL", pgid)
		}
	}

	return nil
}

func isProcessGroupRunning(pgid int) bool {
	// Check if any process in the group is still running
	cmd := exec.Command("ps", "-o", "pid=", "-g", strconv.Itoa(pgid))
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
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

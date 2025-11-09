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

// KillProcessWithVerification kills a process after verifying it matches expected details
// This prevents PID reuse race conditions
func KillProcessWithVerification(pid int, expected ProcessInfo) error {
	// Verify process still matches expected details (prevents PID reuse)
	matches, err := VerifyProcessMatches(pid, expected)
	if err != nil || !matches {
		return fmt.Errorf("process verification failed (PID may have been reused): %w", err)
	}

	return KillProcess(pid)
}

func KillProcess(pid int) error {
	// First verify the process exists and is running
	if !IsProcessRunning(pid) {
		return fmt.Errorf("process %d is not running", pid)
	}

	// Check if process is in uninterruptible sleep (cannot be killed)
	if isUninterruptible, err := IsProcessUninterruptible(pid); err == nil && isUninterruptible {
		state, _ := GetProcessState(pid)
		return fmt.Errorf("process %d is in uninterruptible sleep (state: %s) and cannot be killed. This usually indicates a kernel I/O wait. The process may resolve on its own or require system reboot", pid, state)
	}

	// Check permissions before attempting to kill
	if err := checkPermissionBeforeKill(pid); err != nil {
		return err
	}

	// Try to kill process group first (handles child processes)
	if err := KillProcessGroup(pid); err == nil {
		// Verify process didn't respawn (check for process managers)
		time.Sleep(500 * time.Millisecond)
		if IsProcessRunning(pid) {
			if manager := detectProcessManager(pid); manager != "" {
				return fmt.Errorf("process %d respawned (managed by %s). Stop the service instead: %s", pid, manager, getServiceStopCommand(pid, manager))
			}
		}
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

	// Count processes in group to determine appropriate timeout
	processCount, countErr := countProcessGroupSize(pgid)
	if countErr != nil {
		// If we can't count, use default timeout
		processCount = 1
	}

	// Adaptive timeout: base timeout + additional time per process
	// For large process groups (1000+), allow more time
	// Formula: base 3s + 10ms per process, capped at 30s for very large groups
	adaptiveTimeout := GracefulTerminationTimeout + time.Duration(processCount)*10*time.Millisecond
	maxTimeout := 30 * time.Second
	if adaptiveTimeout > maxTimeout {
		adaptiveTimeout = maxTimeout
	}

	// Minimum timeout of 3 seconds
	if adaptiveTimeout < GracefulTerminationTimeout {
		adaptiveTimeout = GracefulTerminationTimeout
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

	// Wait for graceful termination with adaptive timeout
	deadline := time.Now().Add(adaptiveTimeout)
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

// countProcessGroupSize counts the number of processes in a process group
func countProcessGroupSize(pgid int) (int, error) {
	cmd := exec.Command("ps", "-o", "pid=", "-g", strconv.Itoa(pgid))
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	// Count non-empty lines (each line is a PID)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count, nil
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

// detectProcessManager checks if a process is managed by systemd, supervisor, etc.
func detectProcessManager(pid int) string {
	if runtime.GOOS == "linux" {
		// Check systemd cgroup
		cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
		if data, err := os.ReadFile(cgroupPath); err == nil {
			content := string(data)
			if strings.Contains(content, "systemd") {
				return "systemd"
			}
			if strings.Contains(content, "supervisor") {
				return "supervisor"
			}
		}

		// Check systemd status
		cmd := exec.Command("systemctl", "status", strconv.Itoa(pid))
		if err := cmd.Run(); err == nil {
			return "systemd"
		}
	}

	// Check supervisor
	cmd := exec.Command("supervisorctl", "status", strconv.Itoa(pid))
	if err := cmd.Run(); err == nil {
		return "supervisor"
	}

	return ""
}

// getServiceStopCommand returns the command to stop a service managed by a process manager
func getServiceStopCommand(pid int, manager string) string {
	switch manager {
	case "systemd":
		// Try to get service name from systemd
		cmd := exec.Command("systemctl", "status", strconv.Itoa(pid))
		output, err := cmd.Output()
		if err == nil {
			// Parse service name from output (simplified)
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.Contains(line, ".service") {
					parts := strings.Fields(line)
					for _, part := range parts {
						if strings.HasSuffix(part, ".service") {
							return fmt.Sprintf("systemctl stop %s", part)
						}
					}
				}
			}
		}
		return "systemctl stop <service-name>"
	case "supervisor":
		return "supervisorctl stop <process-name>"
	default:
		return ""
	}
}

package ports

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// VerifyProcessMatches verifies that a process still matches the expected ProcessInfo
// This prevents PID reuse race conditions where a different process might have taken the PID
func VerifyProcessMatches(pid int, expected ProcessInfo) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid PID: %d", pid)
	}

	// Get current process details
	current := getProcessDetails(pid)

	// If we can't get details, assume it doesn't match (safer)
	if current.Cmd == "" && expected.Cmd != "" {
		return false, fmt.Errorf("cannot verify process details")
	}

	// Verify key attributes match
	// 1. Command should match (at least the base command)
	if expected.Cmd != "" && current.Cmd != "" {
		expectedBase := getBaseCommand(expected.Cmd)
		currentBase := getBaseCommand(current.Cmd)
		if expectedBase != currentBase {
			return false, fmt.Errorf("process command changed: expected %s, got %s", expectedBase, currentBase)
		}
	}

	// 2. Working directory should match (if we have it)
	if expected.WorkingDir != "" && current.WorkingDir != "" {
		if expected.WorkingDir != current.WorkingDir {
			return false, fmt.Errorf("process working directory changed: expected %s, got %s", expected.WorkingDir, current.WorkingDir)
		}
	}

	// 3. Start time should be close (within 1 second tolerance for clock skew)
	if !expected.StartTime.IsZero() && !current.StartTime.IsZero() {
		timeDiff := current.StartTime.Sub(expected.StartTime)
		if timeDiff < -1*time.Second || timeDiff > 1*time.Second {
			return false, fmt.Errorf("process start time mismatch: PID may have been reused")
		}
	}

	return true, nil
}

// getBaseCommand extracts the base command name from a full command line
func getBaseCommand(cmd string) string {
	if cmd == "" {
		return ""
	}

	// Remove path and get just the executable name
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ""
	}

	base := parts[0]
	// Remove path if present
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}

	return strings.ToLower(base)
}

// GetProcessState returns the process state (R=running, S=sleeping, D=uninterruptible sleep, etc.)
func GetProcessState(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid PID: %d", pid)
	}

	if runtime.GOOS == "linux" {
		// Read from /proc/PID/stat (field 3 is state)
		statPath := fmt.Sprintf("/proc/%d/stat", pid)
		data, err := os.ReadFile(statPath)
		if err != nil {
			return "", fmt.Errorf("failed to read process stat: %w", err)
		}

		// Parse stat file - format: pid (comm) state ppid ...
		// State is the 3rd field (index 2)
		fields := strings.Fields(string(data))
		if len(fields) < 3 {
			return "", fmt.Errorf("invalid stat file format")
		}

		state := fields[2]
		return state, nil
	} else if runtime.GOOS == "darwin" {
		// macOS: use ps to get state
		cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "state=")
		output, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("failed to get process state: %w", err)
		}

		state := strings.TrimSpace(string(output))
		return state, nil
	}

	return "", fmt.Errorf("process state detection not supported on this platform")
}

// IsProcessUninterruptible checks if a process is in uninterruptible sleep (D state)
func IsProcessUninterruptible(pid int) (bool, error) {
	state, err := GetProcessState(pid)
	if err != nil {
		return false, err
	}

	// D = uninterruptible sleep (usually I/O)
	// Z = zombie (defunct)
	return state == "D" || state == "Z", nil
}


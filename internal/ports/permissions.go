package ports

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"strings"
)

// canKillProcess checks if we have permission to kill the given process
func canKillProcess(pid int) (bool, string, error) {
	if pid <= 0 {
		return false, "", fmt.Errorf("invalid PID: %d", pid)
	}

	// Get current user
	currentUser, err := user.Current()
	if err != nil {
		return false, "", fmt.Errorf("failed to get current user: %w", err)
	}

	// Get process owner
	processUser, err := getProcessOwner(pid)
	if err != nil {
		// If we can't determine owner, assume we can't kill it
		return false, "", fmt.Errorf("failed to get process owner: %w", err)
	}

	// Check if we're the owner
	if processUser.Uid == currentUser.Uid {
		return true, "", nil
	}

	// Check if process is owned by root/system
	if processUser.Uid == "0" {
		return false, "process is owned by root/system - use sudo or contact system administrator", nil
	}

	// Different user - need elevated privileges
	return false, fmt.Sprintf("process is owned by user '%s' (you are '%s') - use sudo to kill", processUser.Username, currentUser.Username), nil
}

// getProcessOwner returns the user who owns the given process
func getProcessOwner(pid int) (*user.User, error) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		// Try to get UID from /proc (Linux) or ps (macOS/Linux)
		uid, err := getProcessUID(pid)
		if err != nil {
			return nil, err
		}

		// Lookup user by UID
		return user.LookupId(uid)
	}

	return nil, fmt.Errorf("unsupported platform")
}

// getProcessUID gets the UID of a process
func getProcessUID(pid int) (string, error) {
	if runtime.GOOS == "linux" {
		// Try /proc first (Linux)
		statPath := fmt.Sprintf("/proc/%d/stat", pid)
		if data, err := os.ReadFile(statPath); err == nil {
			// Parse UID from stat file (field 5)
			fields := strings.Fields(string(data))
			if len(fields) > 4 {
				// UID is in /proc/PID/status, not stat
				statusPath := fmt.Sprintf("/proc/%d/status", pid)
				if statusData, statusErr := os.ReadFile(statusPath); statusErr == nil {
					lines := strings.Split(string(statusData), "\n")
					for _, line := range lines {
						if strings.HasPrefix(line, "Uid:") {
							parts := strings.Fields(line)
							if len(parts) > 1 {
								return parts[1], nil // Real UID is first value
							}
						}
					}
				}
			}
		}
	}

	// Fallback: use ps command
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "uid=")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	uid := strings.TrimSpace(string(output))
	if uid == "" {
		return "", fmt.Errorf("could not determine process UID")
	}

	return uid, nil
}

// checkPermissionBeforeKill verifies we can kill the process and provides helpful error messages
func checkPermissionBeforeKill(pid int) error {
	canKill, reason, err := canKillProcess(pid)
	if err != nil {
		return err
	}

	if !canKill {
		return fmt.Errorf("permission denied: %s", reason)
	}

	return nil
}


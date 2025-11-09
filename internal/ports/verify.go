package ports

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// ProcessVerificationTimeout is the maximum time to wait for process verification
	ProcessVerificationTimeout = 5 * time.Second
)

// VerifyProcessMatches verifies that a process still matches the expected ProcessInfo
// This prevents PID reuse race conditions where a different process might have taken the PID
func VerifyProcessMatches(pid int, expected ProcessInfo) (bool, error) {
	return VerifyProcessMatchesWithContext(context.Background(), pid, expected)
}

// VerifyProcessMatchesWithContext verifies with a context for timeout control
func VerifyProcessMatchesWithContext(ctx context.Context, pid int, expected ProcessInfo) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid PID: %d", pid)
	}

	// Create timeout context if not provided
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), ProcessVerificationTimeout)
		defer cancel()
	}

	// Get current process details with timeout
	type result struct {
		details processDetails
	}
	resultChan := make(chan result, 1)

	go func() {
		details := getProcessDetails(pid)
		resultChan <- result{details: details}
	}()

	var current processDetails
	select {
	case res := <-resultChan:
		current = res.details
	case <-ctx.Done():
		return false, fmt.Errorf("process verification timeout: %w", ctx.Err())
	}

	// If we can't get details, assume it doesn't match (safer)
	if current.Cmd == "" && expected.Cmd != "" {
		return false, fmt.Errorf("cannot verify process details")
	}

	// Verify key attributes match with tolerance for legitimate changes
	// Priority: PID > Working Directory > Start Time > Command

	// 1. Start time should be close (within 1 second tolerance for clock skew)
	// This is the most reliable indicator - if start time matches, it's likely the same process
	startTimeMatches := false
	if !expected.StartTime.IsZero() && !current.StartTime.IsZero() {
		timeDiff := current.StartTime.Sub(expected.StartTime)
		if timeDiff >= -1*time.Second && timeDiff <= 1*time.Second {
			startTimeMatches = true
		}
	} else if expected.StartTime.IsZero() && current.StartTime.IsZero() {
		// Both zero - can't use for verification
		startTimeMatches = true // Don't fail on this
	}

	// 2. Working directory should match (if we have it)
	// This is a strong indicator - processes rarely change working directory
	workingDirMatches := false
	if expected.WorkingDir != "" && current.WorkingDir != "" {
		if expected.WorkingDir == current.WorkingDir {
			workingDirMatches = true
		}
	} else if expected.WorkingDir == "" && current.WorkingDir == "" {
		// Both empty - can't use for verification
		workingDirMatches = true // Don't fail on this
	}

	// 3. Command should match (at least the base command)
	// This is the least reliable - processes can legitimately change command line
	// (e.g., Node.js hot reload, process re-exec with different args)
	commandMatches := false
	if expected.Cmd != "" && current.Cmd != "" {
		expectedBase := getBaseCommand(expected.Cmd)
		currentBase := getBaseCommand(current.Cmd)
		if expectedBase == currentBase {
			commandMatches = true
		}
	} else if expected.Cmd == "" && current.Cmd == "" {
		// Both empty - can't use for verification
		commandMatches = true // Don't fail on this
	}

	// Verification logic: Require at least 2 out of 3 matches, OR
	// if working directory and start time match, allow command to differ
	// (this handles legitimate command line changes)
	matchCount := 0
	if startTimeMatches {
		matchCount++
	}
	if workingDirMatches {
		matchCount++
	}
	if commandMatches {
		matchCount++
	}

	// Require at least 2 matches, OR working dir + start time (allows command changes)
	if matchCount >= 2 {
		return true, nil
	}

	// Special case: if working directory and start time match, allow command to differ
	// This handles processes that legitimately change their command line
	if workingDirMatches && startTimeMatches {
		return true, nil
	}

	// Not enough matches - likely PID reuse
	return false, fmt.Errorf("process verification failed: start_time_match=%v, working_dir_match=%v, command_match=%v (PID may have been reused)", startTimeMatches, workingDirMatches, commandMatches)
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

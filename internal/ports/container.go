package ports

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// IsProcessInContainer checks if a process is running in a container (Docker, LXC, etc.)
func IsProcessInContainer(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid PID: %d", pid)
	}

	if runtime.GOOS == "linux" {
		// Check /proc/PID/cgroup - if it contains "docker", "lxc", "kubepods", etc., it's in a container
		cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
		data, err := os.ReadFile(cgroupPath)
		if err != nil {
			// If we can't read cgroup, assume not in container (safer)
			return false, nil
		}

		cgroupContent := string(data)
		containerIndicators := []string{
			"docker",
			"lxc",
			"kubepods",
			"containerd",
			"crio",
			"systemd",
		}

		// Check if any container indicator is present
		for _, indicator := range containerIndicators {
			if strings.Contains(cgroupContent, indicator) {
				// Additional check: systemd is common, but if it's in a container namespace, it's containerized
				if indicator == "systemd" {
					// Check if it's actually in a container by checking namespace
					if isInContainerNamespace(pid) {
						return true, nil
					}
					continue
				}
				return true, nil
			}
		}

		// Check namespace isolation
		return isInContainerNamespace(pid), nil
	}

	// macOS: containers are less common, but check for Docker Desktop
	if runtime.GOOS == "darwin" {
		// Check if process is in Docker Desktop VM
		// This is a heuristic - Docker Desktop runs in a VM
		cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=")
		output, err := cmd.Output()
		if err == nil {
			cmdStr := strings.ToLower(string(output))
			if strings.Contains(cmdStr, "docker") || strings.Contains(cmdStr, "com.docker") {
				return true, nil
			}
		}
	}

	return false, nil
}

// isInContainerNamespace checks if a process is in a different namespace (container isolation)
func isInContainerNamespace(pid int) bool {
	if runtime.GOOS != "linux" {
		return false
	}

	// Check if process has different mount namespace than init (PID 1)
	// This is a strong indicator of containerization
	mntNS, err := getProcessNamespace(pid, "mnt")
	if err != nil {
		return false
	}

	initMntNS, err := getProcessNamespace(1, "mnt")
	if err != nil {
		return false
	}

	// If mount namespace differs, process is likely in a container
	return mntNS != initMntNS
}

// getProcessNamespace gets the namespace ID for a process
func getProcessNamespace(pid int, nsType string) (string, error) {
	nsPath := fmt.Sprintf("/proc/%d/ns/%s", pid, nsType)
	target, err := os.Readlink(nsPath)
	if err != nil {
		return "", err
	}

	// Namespace link format: type:[inode]
	// Extract inode number
	parts := strings.Split(target, ":")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid namespace link format: %s", target)
	}

	return parts[1], nil
}

// GetProcessNamespaceInfo returns detailed namespace information for a process
func GetProcessNamespaceInfo(pid int) (map[string]string, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("invalid PID: %d", pid)
	}

	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("namespace detection only supported on Linux")
	}

	namespaces := []string{"mnt", "pid", "net", "uts", "ipc", "user", "cgroup"}
	info := make(map[string]string)

	for _, ns := range namespaces {
		nsID, err := getProcessNamespace(pid, ns)
		if err == nil {
			info[ns] = nsID
		}
	}

	return info, nil
}


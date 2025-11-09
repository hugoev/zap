package ports

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type ProcessInfo struct {
	PID        int
	Port       int
	Name       string
	Cmd        string
	User       string
	StartTime  time.Time
	Runtime    time.Duration
	WorkingDir string
}

var commonDevPorts = []int{
	3000, 3001, 3002, 3003, // Node.js, React
	5173, 5174, 5175, // Vite
	8000, 8001, 8080, 8081, // Python, Go, general
	4000, 4001, // SvelteKit
	5000, 5001, // Flask
	4200,       // Angular
	9000, 9001, // Play framework
	7000, 7001, // Phoenix
}

func ScanPorts() ([]ProcessInfo, error) {
	var processes []ProcessInfo

	for _, port := range commonDevPorts {
		procs, err := getProcessesOnPort(port)
		if err != nil {
			continue
		}
		processes = append(processes, procs...)
	}

	return processes, nil
}

func getProcessesOnPort(port int) ([]ProcessInfo, error) {
	var processes []ProcessInfo

	// Use lsof to find processes listening on the port
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-sTCP:LISTEN", "-P", "-n")
	output, err := cmd.Output()
	if err != nil {
		// No process found on this port
		return processes, nil
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return processes, nil
	}

	// Skip header line
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}

		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}

		cmdName := fields[0]
		procInfo := getProcessDetails(pid)

		processes = append(processes, ProcessInfo{
			PID:        pid,
			Port:       port,
			Name:       cmdName,
			Cmd:        procInfo.Cmd,
			User:       procInfo.User,
			StartTime:  procInfo.StartTime,
			Runtime:    procInfo.Runtime,
			WorkingDir: procInfo.WorkingDir,
		})
	}

	return processes, nil
}

type processDetails struct {
	Cmd        string
	User       string
	StartTime  time.Time
	Runtime    time.Duration
	WorkingDir string
}

func getProcessDetails(pid int) processDetails {
	details := processDetails{}

	// Get command line (required, fail silently if can't get it)
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=")
	output, err := cmd.Output()
	if err == nil {
		details.Cmd = strings.TrimSpace(string(output))
	} else {
		// Fallback: just use process name
		details.Cmd = ""
	}

	// Get user (optional, continue if fails)
	cmd = exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "user=")
	output, err = cmd.Output()
	if err == nil {
		details.User = strings.TrimSpace(string(output))
	}

	// Get start time and calculate runtime (optional)
	cmd = exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=")
	output, err = cmd.Output()
	if err == nil {
		startStr := strings.TrimSpace(string(output))
		if startStr != "" {
			if t, err := parseProcessStartTime(startStr); err == nil {
				details.StartTime = t
				details.Runtime = time.Since(t)
			}
		}
	}

	// Get working directory (optional, continue if fails)
	cmd = exec.Command("lsof", "-p", strconv.Itoa(pid), "-a", "-d", "cwd", "-Fn")
	output, err = cmd.Output()
	if err == nil && len(output) > 0 {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "n") {
				details.WorkingDir = strings.TrimPrefix(line, "n")
				break
			}
		}
	}

	return details
}

func parseProcessStartTime(startStr string) (time.Time, error) {
	// ps lstart format: "Mon Jan 2 15:04:05 2006"
	layouts := []string{
		"Mon Jan 2 15:04:05 2006",
		"Mon Jan  2 15:04:05 2006",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, startStr); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", startStr)
}

func IsPortInUse(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return true
	}
	ln.Close()
	return false
}

func IsSafeDevServer(proc ProcessInfo) bool {
	cmdLower := strings.ToLower(proc.Cmd)
	nameLower := strings.ToLower(proc.Name)

	// Node.js dev servers
	if strings.Contains(cmdLower, "node") && (strings.Contains(cmdLower, "vite") ||
		strings.Contains(cmdLower, "next") ||
		strings.Contains(cmdLower, "react") ||
		strings.Contains(cmdLower, "webpack") ||
		strings.Contains(cmdLower, "nodemon") ||
		strings.Contains(cmdLower, "ts-node") ||
		strings.Contains(cmdLower, "tsx")) {
		return true
	}

	// Vite
	if strings.Contains(cmdLower, "vite") {
		return true
	}

	// Python dev servers
	if strings.Contains(cmdLower, "python") && (strings.Contains(cmdLower, "flask") ||
		strings.Contains(cmdLower, "django") ||
		strings.Contains(cmdLower, "uvicorn") ||
		strings.Contains(cmdLower, "gunicorn") ||
		strings.Contains(cmdLower, "runserver")) {
		return true
	}

	// Go dev servers
	if strings.Contains(cmdLower, "go") && (strings.Contains(cmdLower, "run") ||
		strings.Contains(cmdLower, "air")) {
		return true
	}

	// Ruby/Rails
	if strings.Contains(cmdLower, "rails") || strings.Contains(cmdLower, "rackup") {
		return true
	}

	// Elixir/Phoenix
	if strings.Contains(cmdLower, "phoenix") || strings.Contains(cmdLower, "mix phx.server") {
		return true
	}

	// Generic node process on common dev port
	if nameLower == "node" && proc.Port >= 3000 && proc.Port < 9000 {
		return true
	}

	return false
}

func IsInfrastructureProcess(proc ProcessInfo) bool {
	cmdLower := strings.ToLower(proc.Cmd)
	nameLower := strings.ToLower(proc.Name)

	infraKeywords := []string{
		"postgres", "postgresql", "psql",
		"redis", "redis-server",
		"mysql", "mysqld",
		"mongodb", "mongod",
		"docker", "dockerd",
		"rabbitmq",
		"elasticsearch",
		"kafka",
		"consul",
		"etcd",
	}

	for _, keyword := range infraKeywords {
		if strings.Contains(cmdLower, keyword) || strings.Contains(nameLower, keyword) {
			return true
		}
	}

	return false
}

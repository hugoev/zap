package ports

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

type ProcessInfo struct {
	PID  int
	Port int
	Name string
	Cmd  string
}

var commonDevPorts = []int{
	3000, 3001, 3002, 3003, // Node.js, React
	5173, 5174, 5175,       // Vite
	8000, 8001, 8080, 8081, // Python, Go, general
	4000, 4001,             // SvelteKit
	5000, 5001,             // Flask
	4200,                   // Angular
	9000, 9001,             // Play framework
	7000, 7001,             // Phoenix
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
		cmdLine := getProcessCommand(pid)

		processes = append(processes, ProcessInfo{
			PID:  pid,
			Port: port,
			Name: cmdName,
			Cmd:  cmdLine,
		})
	}

	return processes, nil
}

func getProcessCommand(pid int) string {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
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
	if strings.Contains(cmdLower, "node") && (
		strings.Contains(cmdLower, "vite") ||
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
	if strings.Contains(cmdLower, "python") && (
		strings.Contains(cmdLower, "flask") ||
		strings.Contains(cmdLower, "django") ||
		strings.Contains(cmdLower, "uvicorn") ||
		strings.Contains(cmdLower, "gunicorn") ||
		strings.Contains(cmdLower, "runserver")) {
		return true
	}

	// Go dev servers
	if strings.Contains(cmdLower, "go") && (
		strings.Contains(cmdLower, "run") ||
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


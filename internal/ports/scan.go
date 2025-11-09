package ports

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
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
	// Node.js, React, Next.js
	3000, 3001, 3002, 3003, 3004, 3005,
	// Vite, Vite-based frameworks
	5173, 5174, 5175, 5176, 5177,
	// Python (Flask, Django, FastAPI, Uvicorn)
	5000, 5001, 8000, 8001, 8080, 8081, 8888,
	// Go, Rust, general dev servers
	4000, 4001, 4002, 4003,
	// Angular
	4200, 4201,
	// SvelteKit
	5173, 5174,
	// Play framework, Scala
	9000, 9001, 9002,
	// Phoenix, Elixir
	4000, 4001,
	// Ruby on Rails
	3000, 3001,
	// Remix, SvelteKit
	3000, 5173,
	// Bun, Deno
	3000, 8000,
	// Java Spring Boot
	8080, 8081, 8082,
	// .NET
	5000, 5001,
	// Additional common ranges
	7000, 7001, 7002, // Phoenix, LiveView
	6000, 6001, // Additional dev servers
}

func ScanPorts(ctx context.Context) ([]ProcessInfo, error) {
	return ScanPortsRange(ctx, commonDevPorts)
}

// ScanPortsRange scans a specific list of ports (allows custom port ranges)
func ScanPortsRange(ctx context.Context, ports []int) ([]ProcessInfo, error) {
	var processes []ProcessInfo
	var scanErrors []error

	// Limit concurrent goroutines to prevent resource exhaustion
	maxConcurrency := runtime.NumCPU() * 2
	if maxConcurrency > 20 {
		maxConcurrency = 20 // Cap at 20
	}
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}

	// Use goroutines for parallel scanning (faster on multi-core systems)
	type result struct {
		procs []ProcessInfo
		err   error
		port  int
	}

	semaphore := make(chan struct{}, maxConcurrency)
	results := make(chan result, len(ports))
	var wg sync.WaitGroup

	// Launch parallel scans with resource limits
	for _, port := range ports {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		wg.Add(1)
		go func(p int) {
			defer wg.Done()

			// Acquire semaphore (limit concurrency)
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Check for cancellation before scanning
			select {
			case <-ctx.Done():
				results <- result{procs: nil, err: ctx.Err(), port: p}
				return
			default:
			}

			procs, err := getProcessesOnPort(ctx, p)
			results <- result{procs: procs, err: err, port: p}
		}(port)
	}

	// Wait for all goroutines with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Wait for completion or cancellation
	select {
	case <-done:
		// All completed
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("scan timeout exceeded (30s)")
	}

	// Collect results
	close(results)
	for res := range results {
		if res.err != nil {
			// Skip cancellation errors (they're expected)
			if res.err == context.Canceled || res.err == context.DeadlineExceeded {
				continue
			}
			// Log error but continue scanning other ports
			scanErrors = append(scanErrors, fmt.Errorf("port %d: %w", res.port, res.err))
			continue
		}
		processes = append(processes, res.procs...)
	}

	// If we got some processes, return them even if there were some scan errors
	if len(processes) > 0 {
		return processes, nil
	}

	// If no processes found but there were errors, return the first error
	if len(scanErrors) > 0 {
		return nil, fmt.Errorf("scan errors encountered: %w", scanErrors[0])
	}

	return processes, nil
}

func getProcessesOnPort(ctx context.Context, port int) ([]ProcessInfo, error) {
	var processes []ProcessInfo

	// Validate port number
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid port number: %d (must be 1-65535)", port)
	}

	// Try multiple methods for cross-platform compatibility
	// 1. Try lsof first (macOS and most Linux)
	// 2. Fallback to ss (modern Linux)
	// 3. Fallback to netstat (older Linux)

	var output []byte
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Method 1: lsof (macOS, most Linux)
	if lsofPath, err := exec.LookPath("lsof"); err == nil {
		cmd := exec.CommandContext(ctx, lsofPath, "-i", fmt.Sprintf(":%d", port), "-sTCP:LISTEN", "-P", "-n")
		output, err = cmd.Output()
		if err == nil {
			// Success with lsof
			return parseLsofOutput(output, port)
		}
		// If timeout, return error
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout scanning port %d", port)
		}
		// Exit code 1 means no process found (normal)
		if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
			return processes, nil
		}
		// Other lsof errors, try fallback
	}

	// Method 2: ss (modern Linux, faster than netstat)
	if ssPath, err := exec.LookPath("ss"); err == nil {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel2()
		cmd := exec.CommandContext(ctx2, ssPath, "-tlnp", fmt.Sprintf("sport = :%d", port))
		output, err = cmd.Output()
		if err == nil {
			return parseSsOutput(output, port)
		}
		if ctx2.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout scanning port %d", port)
		}
		// Exit code 1 means no process found
		if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
			return processes, nil
		}
	}

	// Method 3: netstat (fallback for older Linux)
	if netstatPath, err := exec.LookPath("netstat"); err == nil {
		ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel3()
		// Try different netstat flags for different systems
		cmd := exec.CommandContext(ctx3, netstatPath, "-tlnp")
		output, err = cmd.Output()
		if err == nil {
			return parseNetstatOutput(output, port)
		}
		if ctx3.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout scanning port %d", port)
		}
	}

	// If all methods failed and we didn't find lsof initially, return error
	if _, err := exec.LookPath("lsof"); err != nil {
		return nil, fmt.Errorf("no port scanning tools found (lsof, ss, or netstat). Please install one of them")
	}

	// If we got here, lsof exists but failed - return the original error
	return nil, fmt.Errorf("failed to scan port %d: %w", port, err)
}

// parseLsofOutput parses lsof output (macOS and most Linux)
func parseLsofOutput(output []byte, port int) ([]ProcessInfo, error) {
	var processes []ProcessInfo
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

// parseSsOutput parses ss output (modern Linux)
func parseSsOutput(output []byte, port int) ([]ProcessInfo, error) {
	var processes []ProcessInfo
	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return processes, nil
	}

	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// ss output format: LISTEN 0 128 *:3000 *:* users:(("node",pid=12345,fd=20))
		// Extract PID from users: section
		pidStart := strings.Index(line, "pid=")
		if pidStart == -1 {
			continue
		}
		pidEnd := strings.Index(line[pidStart+4:], ",")
		if pidEnd == -1 {
			pidEnd = strings.Index(line[pidStart+4:], ")")
		}
		if pidEnd == -1 {
			continue
		}

		pidStr := line[pidStart+4 : pidStart+4+pidEnd]
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		// Extract process name from users: section
		nameStart := strings.Index(line, "(\"")
		nameEnd := strings.Index(line, "\",")
		if nameStart == -1 || nameEnd == -1 {
			nameStart = strings.Index(line, "(")
			nameEnd = strings.Index(line, ",")
		}
		var cmdName string
		if nameStart != -1 && nameEnd != -1 && nameEnd > nameStart {
			cmdName = line[nameStart+2 : nameEnd]
		}

		procInfo := getProcessDetails(pid)
		if cmdName == "" {
			cmdName = procInfo.Cmd
			if spaceIdx := strings.Index(cmdName, " "); spaceIdx > 0 {
				cmdName = cmdName[:spaceIdx]
			}
		}

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

// parseNetstatOutput parses netstat output (older Linux fallback)
func parseNetstatOutput(output []byte, port int) ([]ProcessInfo, error) {
	var processes []ProcessInfo
	lines := strings.Split(string(output), "\n")
	portStr := fmt.Sprintf(":%d", port)

	for _, line := range lines {
		if !strings.Contains(line, portStr) || !strings.Contains(line, "LISTEN") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}

		// netstat format varies, try to extract PID from last field
		// Format: tcp 0 0 0.0.0.0:3000 0.0.0.0:* LISTEN 12345/node
		lastField := fields[len(fields)-1]
		parts := strings.Split(lastField, "/")
		if len(parts) < 2 {
			continue
		}

		pid, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}

		cmdName := parts[1]
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

	// Validate PID
	if pid <= 0 {
		return details
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try to detect platform for optimal ps command
	// macOS uses BSD ps, Linux uses GNU ps (usually)
	// Try BSD format first (works on macOS and some Linux)
	psFormats := []struct {
		cmdFormat string
		args      []string
	}{
		// BSD format (macOS, some Linux)
		{"ps", []string{"-p", strconv.Itoa(pid), "-o", "command="}},
		// GNU format (most Linux)
		{"ps", []string{"-p", strconv.Itoa(pid), "-o", "cmd="}},
	}

	// Get command line
	for _, format := range psFormats {
		cmd := exec.CommandContext(ctx, format.cmdFormat, format.args...)
		output, err := cmd.Output()
		if err == nil && len(output) > 0 {
			details.Cmd = strings.TrimSpace(string(output))
			break
		}
	}

	// Get user (try both formats)
	userFormats := []struct {
		args []string
	}{
		{[]string{"-p", strconv.Itoa(pid), "-o", "user="}},
		{[]string{"-p", strconv.Itoa(pid), "-o", "uid="}},
	}
	for _, format := range userFormats {
		cmd := exec.CommandContext(ctx, "ps", format.args...)
		output, err := cmd.Output()
		if err == nil && len(output) > 0 {
			details.User = strings.TrimSpace(string(output))
			break
		}
	}

	// Get start time and calculate runtime (try multiple formats)
	startFormats := []struct {
		args []string
	}{
		{[]string{"-p", strconv.Itoa(pid), "-o", "lstart="}}, // BSD/macOS
		{[]string{"-p", strconv.Itoa(pid), "-o", "start="}},  // GNU/Linux
	}
	for _, format := range startFormats {
		cmd := exec.CommandContext(ctx, "ps", format.args...)
		output, err := cmd.Output()
		if err == nil && len(output) > 0 {
			startStr := strings.TrimSpace(string(output))
			if startStr != "" {
				if t, err := parseProcessStartTime(startStr); err == nil {
					details.StartTime = t
					details.Runtime = time.Since(t)
					break
				}
			}
		}
	}

	// Get working directory - try multiple methods
	// Method 1: lsof (macOS, most Linux)
	if lsofPath, err := exec.LookPath("lsof"); err == nil {
		cmd := exec.CommandContext(ctx, lsofPath, "-p", strconv.Itoa(pid), "-a", "-d", "cwd", "-Fn")
		output, err := cmd.Output()
		if err == nil && len(output) > 0 {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "n") {
					details.WorkingDir = strings.TrimPrefix(line, "n")
					return details
				}
			}
		}
	}

	// Method 2: pwdx (Linux)
	if pwdxPath, err := exec.LookPath("pwdx"); err == nil {
		cmd := exec.CommandContext(ctx, pwdxPath, strconv.Itoa(pid))
		output, err := cmd.Output()
		if err == nil && len(output) > 0 {
			// pwdx output: "PID: /path/to/dir"
			parts := strings.SplitN(strings.TrimSpace(string(output)), ":", 2)
			if len(parts) == 2 {
				details.WorkingDir = strings.TrimSpace(parts[1])
				return details
			}
		}
	}

	// Method 3: readlink /proc/PID/cwd (Linux)
	procCwd := fmt.Sprintf("/proc/%d/cwd", pid)
	if linkPath, err := os.Readlink(procCwd); err == nil {
		details.WorkingDir = linkPath
	}

	return details
}

func parseProcessStartTime(startStr string) (time.Time, error) {
	// ps lstart format varies by platform:
	// macOS/Linux: "Mon Jan 2 15:04:05 2006" or "Mon Jan  2 15:04:05 2006"
	// Some Linux: "Mon Jan  2 15:04:05 2006" (with extra spaces)
	layouts := []string{
		"Mon Jan 2 15:04:05 2006",
		"Mon Jan  2 15:04:05 2006",  // Single digit day with extra space
		"Mon  Jan 2 15:04:05 2006",  // Extra space after day name
		"Mon  Jan  2 15:04:05 2006", // Both extra spaces
		"2006-01-02 15:04:05",       // ISO format (some systems)
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
	workingDirLower := strings.ToLower(proc.WorkingDir)

	// Node.js dev servers
	nodeDevPatterns := []string{
		"vite", "next", "react", "webpack", "nodemon", "ts-node", "tsx",
		"remix", "svelte", "nuxt", "astro", "gatsby", "parcel",
		"rollup", "esbuild", "swc", "turbo",
	}
	if strings.Contains(cmdLower, "node") {
		for _, pattern := range nodeDevPatterns {
			if strings.Contains(cmdLower, pattern) {
				return true
			}
		}
	}

	// Modern JavaScript runtimes
	if strings.Contains(cmdLower, "bun") || nameLower == "bun" {
		return true
	}
	if strings.Contains(cmdLower, "deno") || nameLower == "deno" {
		return true
	}

	// Vite and Vite-based frameworks
	if strings.Contains(cmdLower, "vite") {
		return true
	}

	// Python dev servers
	pythonDevPatterns := []string{
		"flask", "django", "uvicorn", "gunicorn", "runserver",
		"fastapi", "starlette", "quart", "sanic",
	}
	if strings.Contains(cmdLower, "python") || strings.Contains(cmdLower, "python3") {
		for _, pattern := range pythonDevPatterns {
			if strings.Contains(cmdLower, pattern) {
				return true
			}
		}
	}

	// Go dev servers
	goDevPatterns := []string{"run", "air", "fresh", "fiber", "gin", "echo"}
	if strings.Contains(cmdLower, "go") {
		for _, pattern := range goDevPatterns {
			if strings.Contains(cmdLower, pattern) {
				return true
			}
		}
	}

	// Ruby/Rails
	if strings.Contains(cmdLower, "rails") || strings.Contains(cmdLower, "rackup") ||
		strings.Contains(cmdLower, "puma") || strings.Contains(cmdLower, "unicorn") {
		return true
	}

	// Elixir/Phoenix
	if strings.Contains(cmdLower, "phoenix") || strings.Contains(cmdLower, "mix phx.server") ||
		strings.Contains(cmdLower, "elixir") {
		return true
	}

	// Rust dev servers
	if strings.Contains(cmdLower, "cargo") && (strings.Contains(cmdLower, "run") ||
		strings.Contains(cmdLower, "watch")) {
		return true
	}

	// Java/Kotlin dev servers
	if strings.Contains(cmdLower, "gradle") && strings.Contains(cmdLower, "bootrun") {
		return true
	}
	if strings.Contains(cmdLower, "mvn") && strings.Contains(cmdLower, "spring-boot:run") {
		return true
	}

	// .NET dev servers
	if strings.Contains(cmdLower, "dotnet") && strings.Contains(cmdLower, "watch") {
		return true
	}

	// Check working directory for common dev indicators
	devIndicators := []string{"package.json", "go.mod", "requirements.txt", "pom.xml", "build.gradle"}
	for _, indicator := range devIndicators {
		if strings.Contains(workingDirLower, indicator) {
			// If in a project directory with dev indicators, likely a dev server
			if nameLower == "node" || nameLower == "python" || nameLower == "go" {
				return true
			}
		}
	}

	// Generic node/python/go process on common dev port
	if (nameLower == "node" || nameLower == "python" || nameLower == "python3" || nameLower == "go") &&
		proc.Port >= 3000 && proc.Port < 10000 {
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

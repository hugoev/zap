package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hugoev/zap/internal/cleanup"
	"github.com/hugoev/zap/internal/config"
	"github.com/hugoev/zap/internal/lock"
	"github.com/hugoev/zap/internal/log"
	"github.com/hugoev/zap/internal/ports"
	"github.com/hugoev/zap/internal/version"
)

// commonDevPorts is the default list of ports to scan
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
	// Play framework, Scala
	9000, 9001, 9002,
	// Phoenix, Elixir
	7000, 7001, 7002,
	// Java Spring Boot
	8080, 8081, 8082,
	// .NET
	5000, 5001,
	// Additional common ranges
	6000, 6001,
}

// parsePortRange parses port ranges like "3000-3010,8080,9000-9005"
func parsePortRange(portsStr string) ([]int, error) {
	var ports []int
	seen := make(map[int]bool)

	parts := strings.Split(portsStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if it's a range (e.g., "3000-3010")
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid port range: %s", part)
			}
			start, err := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid start port: %s", rangeParts[0])
			}
			end, err := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid end port: %s", rangeParts[1])
			}
			if start > end {
				return nil, fmt.Errorf("start port (%d) must be <= end port (%d)", start, end)
			}
			if start < 1 || end > 65535 {
				return nil, fmt.Errorf("ports must be in range 1-65535")
			}
			// Add all ports in range
			for p := start; p <= end; p++ {
				if !seen[p] {
					ports = append(ports, p)
					seen[p] = true
				}
			}
		} else {
			// Single port
			port, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid port: %s", part)
			}
			if port < 1 || port > 65535 {
				return nil, fmt.Errorf("port must be in range 1-65535: %d", port)
			}
			if !seen[port] {
				ports = append(ports, port)
				seen[port] = true
			}
		}
	}

	if len(ports) == 0 {
		return nil, fmt.Errorf("no valid ports specified")
	}

	return ports, nil
}

// Version represents a semantic version
type Version struct {
	Major int
	Minor int
	Patch int
}

// parseVersion parses a semantic version string (e.g., "0.3.0", "v0.3.0", "4.1", "4.1.0")
func parseVersion(v string) (Version, error) {
	// Remove 'v' prefix if present
	v = strings.TrimPrefix(v, "v")

	// Normalize: if only MAJOR.MINOR, add .0 for PATCH
	parts := strings.Split(v, ".")
	if len(parts) == 2 {
		v = v + ".0"
		parts = strings.Split(v, ".")
	}

	// Validate format: MAJOR.MINOR.PATCH
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("invalid version format: %s (expected MAJOR.MINOR.PATCH or MAJOR.MINOR)", v)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return Version{}, fmt.Errorf("invalid major version: %s", parts[0])
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return Version{}, fmt.Errorf("invalid minor version: %s", parts[1])
	}

	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return Version{}, fmt.Errorf("invalid patch version: %s", parts[2])
	}

	return Version{Major: major, Minor: minor, Patch: patch}, nil
}

// Compare returns: -1 if v < other, 0 if v == other, 1 if v > other
func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// String returns the version as a string
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// extractVersionFromOutput extracts version from "zap version X.Y.Z" output
func extractVersionFromOutput(output string) (string, error) {
	// Match patterns like "zap version 0.3.0" or "0.3.0"
	re := regexp.MustCompile(`(\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not extract version from: %s", output)
	}
	return matches[1], nil
}

func main() {
	// Acquire single-instance lock
	instanceLock, err := lock.AcquireLock()
	if err != nil {
		log.Log(log.FAIL, err.Error())
		os.Exit(1)
	}
	defer instanceLock.Release()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	cfg, err := config.Load()
	if err != nil {
		log.Log(log.FAIL, "Failed to load config: %v", err)
		os.Exit(1)
	}

	// Create cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Log(log.INFO, "received signal %v, shutting down gracefully...", sig)
		cancel()
	}()

	// Check if zap is in PATH on first run (only for non-version/update commands)
	if command != "version" && command != "update" && command != "help" && command != "h" && command != "--help" && command != "-h" {
		if _, err := exec.LookPath("zap"); err != nil {
			// zap not found in PATH, but we're running it, so check if we should set up PATH
			goBinPath := determineGoBinPath()
			expectedZapPath := filepath.Join(goBinPath, "zap")
			// Check if binary exists in expected location but not in PATH
			if _, err := os.Stat(expectedZapPath); err == nil {
				if !strings.Contains(os.Getenv("PATH"), goBinPath) {
					log.Log(log.INFO, "zap is installed but not in PATH")
					if err := setupPath(goBinPath); err != nil {
						log.VerboseLog("PATH setup failed: %v", err)
					}
				}
			}
		}
	}

	// Parse flags
	flags, flagValues := parseFlags(args)
	yes := flags["yes"] || flags["y"]
	dryRun := flags["dry-run"]
	verbose := flags["verbose"] || flags["v"]
	jsonOutput := flags["json"] || flags["j"]

	// Set verbose mode globally
	log.Verbose = verbose

	switch command {
	case "ports", "port":
		handlePorts(ctx, cfg, yes, dryRun, jsonOutput, flagValues)
	case "cleanup", "clean":
		handleCleanup(cfg, yes, dryRun, jsonOutput, flagValues)
	case "version", "v":
		if jsonOutput {
			fmt.Printf(`{"version":"%s","commit":"%s","date":"%s"}`+"\n", version.Get(), version.GetCommit(), version.GetDate())
		} else {
			fmt.Printf("zap version %s\n", version.Get())
		}
	case "update":
		handleUpdate(instanceLock)
	case "config":
		handleConfig(cfg, args)
	case "help", "h", "--help", "-h":
		printUsage()
	default:
		log.Log(log.FAIL, "Unknown command: %s", command)
		printUsage()
		os.Exit(1)
	}
}

func parseFlags(args []string) (map[string]bool, map[string]string) {
	flags := make(map[string]bool)
	flagValues := make(map[string]string)

	for i, arg := range args {
		if strings.HasPrefix(arg, "--") {
			flag := strings.TrimPrefix(arg, "--")
			// Check if flag has a value (--flag=value or --flag value)
			if strings.Contains(flag, "=") {
				parts := strings.SplitN(flag, "=", 2)
				flagValues[parts[0]] = parts[1]
				flags[parts[0]] = true
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				// Next arg might be a value
				flagValues[flag] = args[i+1]
				flags[flag] = true
			} else {
				flags[flag] = true
			}
		} else if strings.HasPrefix(arg, "-") {
			// Handle short flags like -y, -v
			flag := strings.TrimPrefix(arg, "-")
			for _, char := range flag {
				flags[string(char)] = true
			}
		}
	}
	return flags, flagValues
}

func printUsage() {
	fmt.Println("Usage: zap <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  ports, port    Scan and free up ports")
	fmt.Println("  cleanup, clean  Remove stale dependency/cache folders")
	fmt.Println("  version, v     Show version")
	fmt.Println("  update         Update to latest version")
	fmt.Println("  config         Manage configuration")
	fmt.Println("  help, h        Show this help message")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --yes, -y           Execute without confirmation (safe actions only)")
	fmt.Println("  --dry-run           Preview actions without making changes")
	fmt.Println("  --verbose, -v       Show detailed information")
	fmt.Println("  --json, -j          Output in JSON format (for scripting)")
	fmt.Println("  --ports=<range>     Custom port range (e.g., 3000-3010,8080,9000-9005)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  zap ports --ports=3000-3010,8080")
	fmt.Println("  zap ports --yes")
	fmt.Println("  zap cleanup --dry-run")
	fmt.Println("  zap version --json")
	fmt.Println("  zap config set protected_ports 5432,6379")
}

func handlePorts(ctx context.Context, cfg *config.Config, yes, dryRun, jsonOutput bool, flagValues map[string]string) {
	atomic.AddInt32(&operationActive, 1)
	defer atomic.AddInt32(&operationActive, -1)
	// Check for custom port range
	portsToScan := commonDevPorts
	if portsStr, ok := flagValues["ports"]; ok {
		parsedPorts, err := parsePortRange(portsStr)
		if err != nil {
			log.Log(log.FAIL, "Invalid port range: %v", err)
			os.Exit(1)
		}
		portsToScan = parsedPorts
		log.VerboseLog("scanning custom port range: %v", portsToScan)
	}

	log.Log(log.SCAN, "checking commonly used development ports")
	if log.Verbose {
		log.VerboseLog("scanning ports: %v", portsToScan)
	}

	// Check if required tools are available
	if _, err := exec.LookPath("lsof"); err != nil {
		log.Log(log.FAIL, "lsof command not found. Please install lsof (usually pre-installed on macOS/Linux)")
		os.Exit(1)
	}

	processes, err := ports.ScanPortsRange(ctx, portsToScan)
	if err != nil {
		if err == context.Canceled {
			log.Log(log.INFO, "operation cancelled")
			os.Exit(130) // Standard exit code for SIGINT
		}
		log.Log(log.FAIL, "Failed to scan ports: %v", err)
		os.Exit(1)
	}

	if len(processes) == 0 {
		if jsonOutput {
			fmt.Println(`{"processes":[],"total":0,"safe":0,"infrastructure":0,"skipped":0}`)
		} else {
			log.Log(log.OK, "no processes found on common development ports")
		}
		return
	}

	log.VerboseLog("found %d processes on scanned ports", len(processes))

	// Remove duplicate processes (same PID can appear on multiple ports)
	seenPIDs := make(map[int]bool)
	var uniqueProcesses []ports.ProcessInfo
	for _, proc := range processes {
		if !seenPIDs[proc.PID] {
			seenPIDs[proc.PID] = true
			uniqueProcesses = append(uniqueProcesses, proc)
		} else {
			log.VerboseLog("skipping duplicate PID %d", proc.PID)
		}
	}

	if len(uniqueProcesses) != len(processes) {
		log.VerboseLog("removed %d duplicate process entries", len(processes)-len(uniqueProcesses))
	}

	var safeToKill []ports.ProcessInfo
	var needsConfirmation []ports.ProcessInfo
	var skipped []ports.ProcessInfo

	for _, proc := range uniqueProcesses {
		if cfg.IsPortProtected(proc.Port) {
			log.Log(log.SKIP, ":%d PID %d (%s) protected", proc.Port, proc.PID, proc.Name)
			skipped = append(skipped, proc)
			continue
		}

		// Format process info - always show command and working directory
		runtimeStr := formatRuntime(proc.Runtime)
		procInfo := fmt.Sprintf(":%d PID %d (%s) [%s]", proc.Port, proc.PID, proc.Name, runtimeStr)

		// Always show command preview so user knows what they're killing
		if proc.Cmd != "" {
			cmdPreview := truncateString(proc.Cmd, 60)
			procInfo += fmt.Sprintf(" - %s", cmdPreview)
		} else {
			procInfo += " - (command not available)"
		}

		// Always show working directory
		if proc.WorkingDir != "" {
			procInfo += fmt.Sprintf(" [%s]", truncateString(proc.WorkingDir, 40))
		}

		if ports.IsInfrastructureProcess(proc) {
			needsConfirmation = append(needsConfirmation, proc)
			log.Log(log.FOUND, procInfo)
		} else if ports.IsSafeDevServer(proc) {
			safeToKill = append(safeToKill, proc)
			log.Log(log.FOUND, procInfo)
		} else {
			needsConfirmation = append(needsConfirmation, proc)
			log.Log(log.FOUND, procInfo)
		}
	}

	// Track actual kills
	actualKilledCount := 0

	// Kill safe processes
	if len(safeToKill) > 0 {
		pids := make([]int, len(safeToKill))
		for i, proc := range safeToKill {
			pids[i] = proc.PID
		}

		shouldKill := yes || cfg.AutoConfirmSafeActions
		if !shouldKill && !dryRun {
			showProcessConfirmation("Safe dev servers", safeToKill)
			log.Log(log.ACTION, "terminate %d safe dev server process(es)? (y/N): ", len(safeToKill))
			shouldKill = confirm()
		}

		if shouldKill {
			if dryRun {
				for _, proc := range safeToKill {
					log.Log(log.STOP, "PID %d (would terminate)", proc.PID)
				}
				actualKilledCount += len(safeToKill)
			} else {
				for _, proc := range safeToKill {
					// Verify process is still running before attempting kill
					if !ports.IsProcessRunning(proc.PID) {
						log.VerboseLog("PID %d no longer running, skipping", proc.PID)
						continue
					}

					// Use verification to prevent PID reuse race condition
					if err := ports.KillProcessWithVerification(proc.PID, proc); err != nil {
						log.Log(log.FAIL, "Failed to kill PID %d: %v", proc.PID, err)
						// Continue with other processes
					} else {
						// Verify it was actually killed and port is free
						if !ports.IsProcessRunning(proc.PID) {
							log.Log(log.STOP, "PID %d", proc.PID)
							actualKilledCount++

							// Verify port is actually free (detect immediate reuse)
							time.Sleep(100 * time.Millisecond) // Brief delay for port release
							if ports.IsPortInUse(proc.Port) {
								log.VerboseLog("Port %d immediately reused by another process", proc.Port)
							}
						} else {
							log.Log(log.FAIL, "PID %d still running after kill attempt", proc.PID)
						}
					}
				}
			}
		}
	}

	// Handle processes that need confirmation
	if len(needsConfirmation) > 0 {
		pids := make([]int, len(needsConfirmation))
		for i, proc := range needsConfirmation {
			pids[i] = proc.PID
		}

		shouldKill := yes
		if !shouldKill && !dryRun {
			showProcessConfirmation("Infrastructure/unknown processes", needsConfirmation)
			log.Log(log.ACTION, "terminate %d infrastructure/unknown process(es)? (y/N): ", len(needsConfirmation))
			shouldKill = confirm()
		}

		if shouldKill {
			if dryRun {
				for _, proc := range needsConfirmation {
					log.Log(log.STOP, "PID %d (would terminate)", proc.PID)
				}
				actualKilledCount += len(needsConfirmation)
			} else {
				for _, proc := range needsConfirmation {
					// Verify process is still running before attempting kill
					if !ports.IsProcessRunning(proc.PID) {
						log.VerboseLog("PID %d no longer running, skipping", proc.PID)
						continue
					}

					// Use verification to prevent PID reuse race condition
					if err := ports.KillProcessWithVerification(proc.PID, proc); err != nil {
						log.Log(log.FAIL, "Failed to kill PID %d: %v", proc.PID, err)
						// Continue with other processes
					} else {
						// Verify it was actually killed and port is free
						if !ports.IsProcessRunning(proc.PID) {
							log.Log(log.STOP, "PID %d", proc.PID)
							actualKilledCount++

							// Verify port is actually free (detect immediate reuse)
							time.Sleep(100 * time.Millisecond) // Brief delay for port release
							if ports.IsPortInUse(proc.Port) {
								log.VerboseLog("Port %d immediately reused by another process", proc.Port)
							}
						} else {
							log.Log(log.FAIL, "PID %d still running after kill attempt", proc.PID)
						}
					}
				}
			}
		}
	}

	// Summary statistics - only show success if processes were actually killed
	if actualKilledCount > 0 {
		if dryRun {
			log.Log(log.STATS, "would terminate %d process(es), %d skipped", actualKilledCount, len(skipped))
		} else {
			log.Log(log.STATS, "terminated %d process(es), %d skipped", actualKilledCount, len(skipped))
		}
	} else {
		// No processes were killed
		totalFound := len(safeToKill) + len(needsConfirmation) + len(skipped)
		if totalFound == 0 {
			log.Log(log.OK, "no processes found on common development ports")
		} else if len(skipped) > 0 && len(safeToKill)+len(needsConfirmation) == 0 {
			log.Log(log.OK, "no processes to terminate, %d protected", len(skipped))
		} else {
			log.Log(log.OK, "no processes terminated")
		}
	}
}

func handleCleanup(cfg *config.Config, yes, dryRun, jsonOutput bool, flagValues map[string]string) {
	atomic.AddInt32(&operationActive, 1)
	defer atomic.AddInt32(&operationActive, -1)
	// Validate config
	if cfg.MaxAgeDaysForCleanup <= 0 {
		log.Log(log.FAIL, "Invalid configuration: max_age_days_for_cleanup must be greater than 0")
		os.Exit(1)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Log(log.FAIL, "Failed to get home directory: %v", err)
		os.Exit(1)
	}

	// Auto-detect common development directories
	scanPaths := findProjectDirectories(homeDir)

	if len(scanPaths) == 0 {
		log.Log(log.INFO, "no common project directories found, scanning home directory")
		scanPaths = []string{homeDir}
	} else {
		log.VerboseLog("scanning %d project directory path(s)", len(scanPaths))
	}

	var allDirs []cleanup.DirectoryInfo
	scannedCount := 0

	// Scan directories in parallel for better performance
	type scanResult struct {
		dirs []cleanup.DirectoryInfo
		err  error
		path string
	}

	results := make(chan scanResult, len(scanPaths))

	// Launch parallel scans
	for _, scanPath := range scanPaths {
		if _, err := os.Stat(scanPath); os.IsNotExist(err) {
			log.VerboseLog("skipping non-existent path: %s", scanPath)
			results <- scanResult{dirs: nil, err: nil, path: scanPath}
			continue
		}

		go func(path string) {
			log.VerboseLog("scanning: %s", path)
			progressCallback := func(checkedPath string) {
				if log.Verbose {
					log.VerboseLog("  checking: %s", checkedPath)
				}
			}

			dirs, err := cleanup.ScanDirectories(path, cfg.ShouldCleanup, progressCallback)
			results <- scanResult{dirs: dirs, err: err, path: path}
		}(scanPath)
	}

	// Collect results
	for i := 0; i < len(scanPaths); i++ {
		result := <-results
		if result.err != nil {
			log.VerboseLog("error scanning %s: %v", result.path, result.err)
			continue
		}
		if result.dirs != nil {
			allDirs = append(allDirs, result.dirs...)
			scannedCount++
		}
	}

	log.VerboseLog("scanned %d directory path(s)", scannedCount)

	if len(allDirs) == 0 {
		log.Log(log.OK, "no stale directories found")
		return
	}

	// Display found directories
	totalSize := cleanup.GetTotalSize(allDirs)

	// Sort by size (largest first) for better visibility
	// Use a more efficient sorting algorithm
	sortedDirs := make([]cleanup.DirectoryInfo, len(allDirs))
	copy(sortedDirs, allDirs)

	// Quick sort by size (largest first)
	for i := 0; i < len(sortedDirs)-1; i++ {
		maxIdx := i
		for j := i + 1; j < len(sortedDirs); j++ {
			if sortedDirs[j].Size > sortedDirs[maxIdx].Size {
				maxIdx = j
			}
		}
		if maxIdx != i {
			sortedDirs[i], sortedDirs[maxIdx] = sortedDirs[maxIdx], sortedDirs[i]
		}
	}

	log.Log(log.FOUND, "found %d directories (%s total)", len(allDirs), cleanup.FormatSize(totalSize))

	for _, dir := range sortedDirs {
		age := int(time.Since(dir.ModTime).Hours() / 24)
		log.Log(log.FOUND, "%s (%s, %d days old)", dir.Path, cleanup.FormatSize(dir.Size), age)
	}

	shouldDelete := yes
	if !shouldDelete && !dryRun {
		showDirectoryConfirmation(sortedDirs, totalSize)
		log.Log(log.ACTION, "delete these %d directories (%s total)? (y/N): ", len(allDirs), cleanup.FormatSize(totalSize))
		shouldDelete = confirm()
	}

	if shouldDelete {
		if dryRun {
			log.Log(log.INFO, "would delete %d directories (%s total)", len(allDirs), cleanup.FormatSize(totalSize))
			for _, dir := range sortedDirs {
				log.Log(log.DELETE, "%s (would delete)", dir.Path)
			}
		} else {
			deletedCount := 0
			freedSize := int64(0)
			failedCount := 0

			for _, dir := range allDirs {
				// Verify directory still exists before attempting deletion
				if _, err := os.Stat(dir.Path); os.IsNotExist(err) {
					log.VerboseLog("%s no longer exists, skipping", dir.Path)
					continue
				}

				if err := cleanup.DeleteDirectory(dir.Path); err != nil {
					log.Log(log.FAIL, "Failed to delete %s: %v", dir.Path, err)
					failedCount++
				} else {
					// Verify deletion succeeded
					if _, err := os.Stat(dir.Path); os.IsNotExist(err) {
						log.Log(log.DELETE, "%s", dir.Path)
						deletedCount++
						freedSize += dir.Size
					} else {
						log.Log(log.FAIL, "Deletion verification failed for %s", dir.Path)
						failedCount++
					}
				}
			}

			if failedCount > 0 {
				log.Log(log.STATS, "deleted %d directories, freed %s (%d failed)", deletedCount, cleanup.FormatSize(freedSize), failedCount)
			} else {
				log.Log(log.STATS, "deleted %d directories, freed %s", deletedCount, cleanup.FormatSize(freedSize))
			}
		}
	}
}

func confirm() bool {
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		// If stdin is closed or there's an error, default to no
		return false
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// showProcessConfirmation displays detailed information about processes before asking for confirmation
func showProcessConfirmation(category string, processes []ports.ProcessInfo) {
	fmt.Println()
	fmt.Printf("  %s (%d):\n", category, len(processes))
	for i, proc := range processes {
		runtimeStr := formatRuntime(proc.Runtime)
		cmdPreview := truncateString(proc.Cmd, 50)
		dirPreview := truncateString(proc.WorkingDir, 35)

		fmt.Printf("    %d. :%d PID %d (%s) [%s]", i+1, proc.Port, proc.PID, proc.Name, runtimeStr)
		if cmdPreview != "" {
			fmt.Printf(" - %s", cmdPreview)
		}
		if dirPreview != "" {
			fmt.Printf(" [%s]", dirPreview)
		}
		fmt.Println()
	}
	fmt.Println()
}

// showDirectoryConfirmation displays detailed information about directories before asking for confirmation
func showDirectoryConfirmation(dirs []cleanup.DirectoryInfo, totalSize int64) {
	fmt.Println()
	fmt.Printf("  Directories to delete (%d, %s total):\n", len(dirs), cleanup.FormatSize(totalSize))

	// Show all directories
	for i, dir := range dirs {
		age := int(time.Since(dir.ModTime).Hours() / 24)
		fmt.Printf("    %d. %s (%s, %d days old)\n", i+1, dir.Path, cleanup.FormatSize(dir.Size), age)
	}
	fmt.Println()
}

func formatRuntime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	} else {
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd", days)
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// determineGoBinPath determines where Go installs binaries
func determineGoBinPath() string {
	goBinPath := os.Getenv("GOBIN")
	if goBinPath != "" {
		return goBinPath
	}
	gopath := os.Getenv("GOPATH")
	if gopath != "" {
		return filepath.Join(gopath, "bin")
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, "go", "bin")
}

// setupPath automatically configures PATH for the user's shell
func setupPath(goBinPath string) error {
	// Check if already in PATH
	currentPath := os.Getenv("PATH")
	if strings.Contains(currentPath, goBinPath) {
		return nil // Already configured
	}

	// Detect shell
	shell := os.Getenv("SHELL")
	if shell == "" {
		// Fallback: try to detect from common shells
		shell = "/bin/bash" // Default fallback
	}

	// Determine config file based on shell
	var configFile string
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	shellName := filepath.Base(shell)
	switch shellName {
	case "bash":
		// Try .bash_profile first (macOS), then .bashrc (Linux)
		if runtime.GOOS == "darwin" {
			configFile = filepath.Join(homeDir, ".bash_profile")
		} else {
			configFile = filepath.Join(homeDir, ".bashrc")
		}
		// Fallback to .bashrc if .bash_profile doesn't exist
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			configFile = filepath.Join(homeDir, ".bashrc")
		}
	case "zsh":
		configFile = filepath.Join(homeDir, ".zshrc")
	case "fish":
		configDir := filepath.Join(homeDir, ".config", "fish")
		os.MkdirAll(configDir, 0755)
		configFile = filepath.Join(configDir, "config.fish")
	default:
		// Unknown shell, provide instructions instead
		log.Log(log.INFO, "detected shell: %s (not automatically configurable)", shellName)
		showPathInstructions(goBinPath, shellName)
		return nil
	}

	// Check if path is already in config file
	if pathAlreadyInConfig(configFile, goBinPath) {
		log.Log(log.INFO, "PATH already configured in %s", configFile)
		log.Log(log.INFO, "run 'source %s' or restart your terminal to use zap", configFile)
		return nil
	}

	// Ask user if they want to add it
	fmt.Println()
	log.Log(log.ACTION, "add %s to PATH in %s? (y/N): ", goBinPath, configFile)
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return nil
	}
	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		showPathInstructions(goBinPath, shellName)
		return nil
	}

	// Add to config file
	pathLine := fmt.Sprintf("\nexport PATH=\"$PATH:%s\"\n", goBinPath)

	// Read existing file
	existingContent, err := os.ReadFile(configFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", configFile, err)
	}

	// Check if it's already there (just in case)
	if strings.Contains(string(existingContent), goBinPath) {
		log.Log(log.INFO, "PATH already configured in %s", configFile)
		return nil
	}

	// Append to file
	file, err := os.OpenFile(configFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", configFile, err)
	}
	defer file.Close()

	// Add comment and PATH line
	comment := "\n# Added by zap - Go bin directory\n"
	if _, err := file.WriteString(comment + pathLine); err != nil {
		return fmt.Errorf("failed to write to %s: %w", configFile, err)
	}

	log.Log(log.OK, "added %s to PATH in %s", goBinPath, configFile)
	log.Log(log.INFO, "run 'source %s' or restart your terminal to use zap", configFile)

	return nil
}

func pathAlreadyInConfig(configFile, path string) bool {
	content, err := os.ReadFile(configFile)
	if err != nil {
		return false
	}
	return strings.Contains(string(content), path)
}

func validatePath(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Basic validation - ensure no shell injection characters
	if strings.ContainsAny(path, "\n\r\t$`\"'\\") {
		return fmt.Errorf("path contains invalid characters")
	}

	return nil
}

func shellEscape(s string) string {
	// Remove any shell metacharacters and wrap in single quotes
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}

// copyFile copies a file from src to dst, preserving permissions
// getBinaryArchitecture determines the architecture of a compiled binary
func getBinaryArchitecture(binaryPath string) (string, error) {
	if runtime.GOOS == "windows" {
		// Windows: use file command or PE header parsing
		// For now, assume it matches runtime.GOARCH if we can't determine
		return runtime.GOARCH, nil
	}

	// Unix: use file command to determine architecture
	cmd := exec.Command("file", binaryPath)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run file command: %w", err)
	}

	fileOutput := strings.ToLower(string(output))

	// Parse architecture from file output
	// Examples:
	// "ELF 64-bit LSB executable, x86-64" -> "amd64"
	// "Mach-O 64-bit executable arm64" -> "arm64"
	// "ELF 64-bit LSB executable, ARM aarch64" -> "arm64"

	if strings.Contains(fileOutput, "x86-64") || strings.Contains(fileOutput, "x86_64") {
		return "amd64", nil
	}
	if strings.Contains(fileOutput, "aarch64") || strings.Contains(fileOutput, "arm64") {
		return "arm64", nil
	}
	if strings.Contains(fileOutput, "arm") && !strings.Contains(fileOutput, "arm64") {
		return "arm", nil
	}
	if strings.Contains(fileOutput, "386") || strings.Contains(fileOutput, "i386") {
		return "386", nil
	}
	if strings.Contains(fileOutput, "ppc64") {
		return "ppc64", nil
	}
	if strings.Contains(fileOutput, "mips") {
		return "mips", nil
	}

	// If we can't determine, return error
	return "", fmt.Errorf("unable to determine architecture from file output: %s", fileOutput)
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	// Get source file info for permissions
	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Create destination file with same permissions
	destFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, sourceInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	// Copy contents
	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	// Sync to ensure data is written
	if err := destFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync destination file: %w", err)
	}

	return nil
}

func showPathInstructions(goBinPath, shellName string) {
	fmt.Println()
	log.Log(log.INFO, "to add %s to your PATH manually:", goBinPath)

	// Escape path for display
	escapedPath := shellEscape(goBinPath)

	switch shellName {
	case "bash":
		if runtime.GOOS == "darwin" {
			log.Log(log.INFO, "  echo 'export PATH=\"$PATH:%s\"' >> ~/.bash_profile", escapedPath)
			log.Log(log.INFO, "  source ~/.bash_profile")
		} else {
			log.Log(log.INFO, "  echo 'export PATH=\"$PATH:%s\"' >> ~/.bashrc", escapedPath)
			log.Log(log.INFO, "  source ~/.bashrc")
		}
	case "zsh":
		log.Log(log.INFO, "  echo 'export PATH=\"$PATH:%s\"' >> ~/.zshrc", escapedPath)
		log.Log(log.INFO, "  source ~/.zshrc")
	case "fish":
		log.Log(log.INFO, "  echo 'set -gx PATH $PATH %s' >> ~/.config/fish/config.fish", escapedPath)
		log.Log(log.INFO, "  source ~/.config/fish/config.fish")
	default:
		log.Log(log.INFO, "  add %s to your PATH in your shell configuration file", goBinPath)
	}
	fmt.Println()
}

func getCommonPorts() []int {
	return []int{
		3000, 3001, 3002, 3003,
		5173, 5174, 5175,
		8000, 8001, 8080, 8081,
		4000, 4001,
		5000, 5001,
		4200,
		9000, 9001,
		7000, 7001,
	}
}

// findProjectDirectories auto-detects common project directory locations
func findProjectDirectories(homeDir string) []string {
	var paths []string

	// Common project directory names (case-insensitive on macOS)
	candidates := []string{
		"Documents", "Projects", "Code", "workspace", "work",
		"Development", "dev", "src", "repos", "repositories",
		"git", "github", "gitlab", "bitbucket",
	}

	for _, name := range candidates {
		path := filepath.Join(homeDir, name)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			paths = append(paths, path)
		}
	}

	// Also check common macOS locations
	if runtime.GOOS == "darwin" {
		macPaths := []string{
			filepath.Join(homeDir, "Desktop"),
		}
		for _, path := range macPaths {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				paths = append(paths, path)
			}
		}
	}

	return paths
}

// isOperationActive checks if zap is currently performing a ports or cleanup operation
// This prevents updates during active operations which could corrupt state
var operationActive int32 // atomic counter for active operations

func handleUpdate(instanceLock *lock.InstanceLock) {
	// Check if any operations are active
	if atomic.LoadInt32(&operationActive) > 0 {
		log.Log(log.FAIL, "cannot update while operations are in progress")
		log.Log(log.INFO, "please wait for current operation to complete")
		os.Exit(1)
	}
	log.Log(log.SCAN, "checking for updates...")

	// Check all required dependencies upfront with helpful messages
	dependencies := map[string]struct {
		installMsg string
		url        string
	}{
		"go": {
			installMsg: "Go is required for updates",
			url:        "https://golang.org/dl/",
		},
		"git": {
			installMsg: "Git is required to fetch version tags",
			url:        "https://git-scm.com/downloads",
		},
	}

	for cmd, info := range dependencies {
		if _, err := exec.LookPath(cmd); err != nil {
			log.Log(log.FAIL, "%s not found in PATH", cmd)
			log.Log(log.INFO, "%s. Install from: %s", info.installMsg, info.url)
			os.Exit(1)
		}
	}

	goPath, _ := exec.LookPath("go")
	log.VerboseLog("using go at: %s", goPath)

	// Get current version
	currentVersion := version.Get()
	log.Log(log.INFO, "current version: %s", currentVersion)

	// Check the installed binary's modification time to see if it was recently updated
	var originalModTime time.Time
	var originalZapPath string
	zapPath, pathErr := exec.LookPath("zap")
	if pathErr == nil {
		originalZapPath = zapPath
		if info, statErr := os.Stat(zapPath); statErr == nil {
			originalModTime = info.ModTime()
			// If binary was modified in the last minute, assume it's already up to date
			if time.Since(originalModTime) < time.Minute {
				log.Log(log.OK, "already up to date (version %s)", version.Get())
				log.VerboseLog("binary was recently updated")
				return
			}
		}
	}

	// Determine where go install will put the binary
	goBinPath := os.Getenv("GOBIN")
	if goBinPath == "" {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			homeDir, _ := os.UserHomeDir()
			goBinPath = filepath.Join(homeDir, "go", "bin")
		} else {
			goBinPath = filepath.Join(gopath, "bin")
		}
	}
	expectedZapPath := filepath.Join(goBinPath, "zap")

	// Warn if current binary is not in the Go bin directory
	if originalZapPath != "" && originalZapPath != expectedZapPath {
		log.VerboseLog("current binary at: %s", originalZapPath)
		log.VerboseLog("will install to: %s", expectedZapPath)
	}

	// Try to get the latest commit info (optional, don't fail if it doesn't work)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Version}}", "github.com/hugoev/zap@main")
	output, err := cmd.Output()
	latestModuleVersion := strings.TrimSpace(string(output))

	if err != nil || latestModuleVersion == "" {
		log.VerboseLog("could not determine latest version, proceeding with update...")
	} else {
		log.VerboseLog("latest module version: %s", latestModuleVersion)
	}

	// Try to get the latest version tag from GitHub with retry logic
	var installTarget string
	var latestTag string
	var latestVersion Version

	maxRetries := 5
	baseDelay := 1 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)

		tagCmd := exec.CommandContext(ctx2, "git", "ls-remote", "--tags", "--sort=-v:refname", "https://github.com/hugoev/zap.git", "v*")
		tagOutput, tagErr := tagCmd.Output()
		cancel2()

		if tagErr == nil && len(tagOutput) > 0 {
			// Parse all tags and find the latest valid semantic version
			lines := strings.Split(strings.TrimSpace(string(tagOutput)), "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) == "" {
					continue
				}
				// Extract tag name from line like "refs/tags/v0.3.0" or "refs/tags/v0.3.0^{}"
				parts := strings.Fields(line)
				if len(parts) < 2 {
					continue
				}
				tagRef := parts[1]
				if strings.HasPrefix(tagRef, "refs/tags/") {
					tag := strings.TrimPrefix(tagRef, "refs/tags/")
					// Remove ^{} suffix if present (dereferenced tag pointer)
					tag = strings.TrimSuffix(tag, "^{}")
					// Skip if not a version tag
					if !strings.HasPrefix(tag, "v") {
						continue
					}
					// Try to parse as semantic version
					if ver, err := parseVersion(tag); err == nil {
						// Found a valid version, check if it's newer
						if installTarget == "" || ver.Compare(latestVersion) > 0 {
							latestTag = tag
							latestVersion = ver
							installTarget = fmt.Sprintf("github.com/hugoev/zap/cmd/zap@%s", tag)
						}
					}
				}
			}

			if installTarget != "" {
				log.VerboseLog("found latest tag: %s (version %s)", latestTag, latestVersion)
				break
			}
		}

		if attempt < maxRetries {
			// Exponential backoff: 1s, 2s, 4s, 8s, 16s
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			log.VerboseLog("network error (attempt %d/%d), retrying in %v...", attempt, maxRetries, delay)
			time.Sleep(delay)
		} else {
			log.VerboseLog("failed to fetch tags after %d attempts", maxRetries)
		}
	}

	// Compare with current version
	currentVer, parseErr := parseVersion(version.Get())
	if parseErr == nil && installTarget != "" {
		if latestVersion.Compare(currentVer) <= 0 {
			log.Log(log.OK, "already up to date (version %s)", version.Get())
			return
		}
		log.VerboseLog("update available: %s -> %s", version.Get(), latestVersion)
	}

	// Fallback to @main if we can't get tags
	if installTarget == "" {
		installTarget = "github.com/hugoev/zap/cmd/zap@main"
		log.VerboseLog("using main branch as fallback")
		latestTag = "main" // Set latestTag for fallback case
	}

	// Install latest version with version injection
	// go install doesn't support ldflags, so we need to build manually
	log.Log(log.INFO, "downloading and installing latest version...")

	// Only try git clone if we have a valid tag (not "main")
	if latestTag != "" && latestTag != "main" {
		// Create temp directory for cloning
		tempDir, err := os.MkdirTemp("", "zap-update-*")
		if err != nil {
			log.Log(log.FAIL, "failed to create temp directory: %v", err)
			os.Exit(1)
		}
		defer os.RemoveAll(tempDir)

		// Clone the repo at the specific tag
		log.VerboseLog("cloning repository at tag %s...", latestTag)
		cloneCtx, cloneCancel := context.WithTimeout(context.Background(), 30*time.Second)
		cloneCmd := exec.CommandContext(cloneCtx, "git", "clone", "--depth", "1", "--branch", latestTag, "https://github.com/hugoev/zap.git", tempDir)
		cloneOutput, cloneErr := cloneCmd.CombinedOutput()
		cloneCancel()

		if cloneErr != nil {
			log.VerboseLog("failed to clone with tag, trying full clone: %s", string(cloneOutput))
			// Fallback: clone main and checkout tag
			cloneCtx2, cloneCancel2 := context.WithTimeout(context.Background(), 30*time.Second)
			cloneCmd2 := exec.CommandContext(cloneCtx2, "git", "clone", "--depth", "1", "https://github.com/hugoev/zap.git", tempDir)
			cloneOutput2, cloneErr2 := cloneCmd2.CombinedOutput()
			cloneCancel2()

			if cloneErr2 != nil {
				log.Log(log.FAIL, "failed to clone repository: %s", string(cloneOutput2))
				log.Log(log.INFO, "falling back to go install (version may show as 'dev')")
				// Fallback to regular go install
				updateCtx, updateCancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer updateCancel()
				cmd = exec.CommandContext(updateCtx, "go", "install", installTarget)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					log.Log(log.FAIL, "failed to install: %v", err)
					os.Exit(1)
				}
				return // Exit early if we used fallback
			} else {
				// Checkout the tag
				log.VerboseLog("checking out tag %s...", latestTag)
				checkoutCtx, checkoutCancel := context.WithTimeout(context.Background(), 10*time.Second)
				checkoutCmd := exec.CommandContext(checkoutCtx, "git", "-C", tempDir, "checkout", latestTag)
				checkoutOutput, checkoutErr := checkoutCmd.CombinedOutput()
				checkoutCancel()
				if checkoutErr != nil {
					log.Log(log.FAIL, "failed to checkout tag: %s", string(checkoutOutput))
					log.Log(log.INFO, "falling back to go install (version may show as 'dev')")
					// Fallback
					updateCtx, updateCancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer updateCancel()
					cmd = exec.CommandContext(updateCtx, "go", "install", installTarget)
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					if err := cmd.Run(); err != nil {
						log.Log(log.FAIL, "failed to install: %v", err)
						os.Exit(1)
					}
					return // Exit early if we used fallback
				}
			}
		}

		// Build with version injection
		log.VerboseLog("building with version injection...")
		buildCtx, buildCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer buildCancel()

		versionStr := strings.TrimPrefix(latestTag, "v") // Remove 'v' prefix
		commitCtx, commitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		commitCmd := exec.CommandContext(commitCtx, "git", "-C", tempDir, "rev-parse", "--short", "HEAD")
		commitOutput, _ := commitCmd.Output()
		commitCancel()
		commitHash := strings.TrimSpace(string(commitOutput))
		if commitHash == "" {
			commitHash = "unknown"
		}

		dateStr := time.Now().UTC().Format("2006-01-02T15:04:05Z")

		ldflags := fmt.Sprintf("-X github.com/hugoev/zap/internal/version.Version=%s -X github.com/hugoev/zap/internal/version.Commit=%s -X github.com/hugoev/zap/internal/version.Date=%s",
			versionStr, commitHash, dateStr)

		log.VerboseLog("building with version: %s, commit: %s", versionStr, commitHash)

		// Build to temporary location first (safety: don't replace existing binary until verified)
		tempBinaryPath := expectedZapPath + ".new"
		buildCmd := exec.CommandContext(buildCtx, "go", "build", "-ldflags", ldflags, "-o", tempBinaryPath, "./cmd/zap")
		buildCmd.Dir = tempDir
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		buildErr := buildCmd.Run()
		buildCancel()

		if buildErr != nil {
			// Clean up temp binary on build failure
			os.Remove(tempBinaryPath)
			log.Log(log.FAIL, "failed to build update: %v", buildErr)
			log.Log(log.INFO, "falling back to go install (version may show as 'dev')")
			// Fallback to regular go install
			updateCtx, updateCancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer updateCancel()
			cmd = exec.CommandContext(updateCtx, "go", "install", installTarget)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				log.Log(log.FAIL, "failed to install: %v", err)
				os.Exit(1)
			}
		} else {
			// Make the binary executable
			os.Chmod(tempBinaryPath, 0755)
			log.VerboseLog("built binary with version %s at %s", versionStr, tempBinaryPath)

			// Verify architecture matches before proceeding
			log.VerboseLog("verifying binary architecture...")
			currentArch := runtime.GOARCH
			binaryArch, archErr := getBinaryArchitecture(tempBinaryPath)
			if archErr != nil {
				log.VerboseLog("could not determine binary architecture: %v", archErr)
			} else if binaryArch != currentArch {
				os.Remove(tempBinaryPath)
				log.Log(log.FAIL, "architecture mismatch: binary is %s, system is %s", binaryArch, currentArch)
				log.Log(log.INFO, "update aborted - architecture mismatch")
				os.Exit(1)
			}

			// Verify the new binary works before replacing the old one
			// Temporarily release the lock so the new binary can acquire it during verification
			log.VerboseLog("verifying new binary...")
			if instanceLock != nil {
				log.VerboseLog("temporarily releasing lock for verification...")
				instanceLock.Release()
			}
			
			verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 10*time.Second)
			verifyCmd := exec.CommandContext(verifyCtx, tempBinaryPath, "version")
			verifyOutput, verifyErr := verifyCmd.Output()
			verifyCancel()
			
			// Re-acquire the lock immediately after verification
			if instanceLock != nil {
				log.VerboseLog("re-acquiring lock after verification...")
				var reacquireErr error
				instanceLock, reacquireErr = lock.AcquireLock()
				if reacquireErr != nil {
					// Couldn't re-acquire lock - another instance might have started
					os.Remove(tempBinaryPath)
					log.Log(log.FAIL, "failed to re-acquire lock after verification: %v", reacquireErr)
					log.Log(log.INFO, "update aborted - another instance may have started")
					os.Exit(1)
				}
			}

			if verifyErr != nil {
				// New binary is corrupted or doesn't work - don't replace
				os.Remove(tempBinaryPath)
				log.Log(log.FAIL, "new binary verification failed: %v", verifyErr)
				log.Log(log.INFO, "update aborted - existing binary unchanged")
				log.Log(log.INFO, "output: %s", string(verifyOutput))
				os.Exit(1)
			}

			// Binary works - create backup of existing binary if it exists
			var backupPath string
			if _, err := os.Stat(expectedZapPath); err == nil {
				backupPath = expectedZapPath + ".backup"
				log.VerboseLog("creating backup of existing binary: %s", backupPath)
				if err := copyFile(expectedZapPath, backupPath); err != nil {
					os.Remove(tempBinaryPath)
					log.Log(log.FAIL, "failed to create backup: %v", err)
					log.Log(log.INFO, "update aborted - cannot backup existing binary")
					os.Exit(1)
				}
			}

			// Replace old binary with new one (atomic on most filesystems)
			log.VerboseLog("replacing binary: %s -> %s", tempBinaryPath, expectedZapPath)
			if err := os.Rename(tempBinaryPath, expectedZapPath); err != nil {
				// Replacement failed - restore backup if we created one
				os.Remove(tempBinaryPath)
				if backupPath != "" {
					log.Log(log.FAIL, "failed to replace binary: %v", err)
					log.Log(log.INFO, "restoring from backup...")
					if restoreErr := copyFile(backupPath, expectedZapPath); restoreErr != nil {
						log.Log(log.FAIL, "failed to restore backup: %v", restoreErr)
						log.Log(log.INFO, "original binary may be corrupted - manual recovery required")
					} else {
						log.Log(log.INFO, "backup restored successfully")
					}
				} else {
					log.Log(log.FAIL, "failed to replace binary: %v", err)
				}
				os.Exit(1)
			}

			// Verify the replaced binary still works
			// Temporarily release lock for final verification
			log.VerboseLog("verifying replaced binary...")
			if instanceLock != nil {
				log.VerboseLog("temporarily releasing lock for final verification...")
				instanceLock.Release()
			}
			
			finalVerifyCtx, finalVerifyCancel := context.WithTimeout(context.Background(), 10*time.Second)
			finalVerifyCmd := exec.CommandContext(finalVerifyCtx, expectedZapPath, "version")
			finalVerifyOutput, finalVerifyErr := finalVerifyCmd.Output()
			finalVerifyCancel()
			
			// Re-acquire lock after final verification
			if instanceLock != nil {
				log.VerboseLog("re-acquiring lock after final verification...")
				var reacquireErr error
				instanceLock, reacquireErr = lock.AcquireLock()
				if reacquireErr != nil {
					log.Log(log.INFO, "warning: could not re-acquire lock after final verification (another instance may have started)")
					// Don't fail - update is complete
				}
			}

			if finalVerifyErr != nil {
				// Replacement corrupted the binary - restore from backup
				log.Log(log.FAIL, "replaced binary verification failed: %v", finalVerifyErr)
				if backupPath != "" {
					log.Log(log.INFO, "restoring from backup...")
					if restoreErr := copyFile(backupPath, expectedZapPath); restoreErr != nil {
						log.Log(log.FAIL, "failed to restore backup: %v", restoreErr)
						log.Log(log.INFO, "original binary may be corrupted - manual recovery required")
					} else {
						log.Log(log.INFO, "backup restored successfully")
					}
				} else {
					log.Log(log.FAIL, "no backup available - binary may be corrupted")
				}
				os.Exit(1)
			}

			// Success - clean up backup (optional, keep for safety)
			log.VerboseLog("update successful - new binary verified")
			log.VerboseLog("new version output: %s", strings.TrimSpace(string(finalVerifyOutput)))
			// Keep backup for now (user can clean it up later if needed)
			if backupPath != "" {
				log.VerboseLog("backup kept at: %s (safe to delete)", backupPath)
			}
		}
	} else {
		// No tag available, fallback to go install
		log.VerboseLog("no version tag available, using go install (version may show as 'dev')")
		updateCtx, updateCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer updateCancel()
		cmd = exec.CommandContext(updateCtx, "go", "install", installTarget)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Log(log.FAIL, "failed to install: %v", err)
			os.Exit(1)
		}
	}

	// Verify the update by checking the new binary's version
	// Give filesystem a moment to sync (only needed for go install fallback)
	if latestTag == "" || latestTag == "main" {
		time.Sleep(200 * time.Millisecond)
	}

	// Check if PATH needs to be configured
	if !strings.Contains(os.Getenv("PATH"), goBinPath) {
		log.Log(log.INFO, "setting up PATH...")
		if err := setupPath(goBinPath); err != nil {
			log.VerboseLog("PATH setup failed: %v", err)
			log.Log(log.INFO, "add %s to your PATH manually to use the updated version", goBinPath)
		}
	}

	// Check the installed binary (in Go bin directory)
	var installedZapPath string
	if _, err := os.Stat(expectedZapPath); err == nil {
		installedZapPath = expectedZapPath
	} else if pathErr == nil {
		// Fallback to checking the original path
		installedZapPath = originalZapPath
	}

	// Check if binary was updated by comparing modification times
	if installedZapPath != "" {
		if newInfo, err := os.Stat(installedZapPath); err == nil {
			if !originalModTime.IsZero() && newInfo.ModTime().After(originalModTime) {
				// Binary was updated, verify by running it and checking version
				verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
				verifyCmd := exec.CommandContext(verifyCtx, installedZapPath, "version")
				verifyOutput, verifyErr := verifyCmd.Output()
				verifyCancel()

				if verifyErr == nil {
					outputStr := strings.TrimSpace(string(verifyOutput))

					// Extract and compare versions
					newVerStr, extractErr := extractVersionFromOutput(outputStr)
					if extractErr == nil {
						newVer, parseErr := parseVersion(newVerStr)
						if parseErr == nil {
							currentVer, _ := parseVersion(version.Get())
							if newVer.Compare(currentVer) > 0 {
								log.Log(log.OK, "update complete!")
								log.Log(log.INFO, "upgraded from %s to %s", version.Get(), newVer)
							} else if newVer.Compare(currentVer) == 0 {
								log.Log(log.OK, "update complete!")
								log.Log(log.INFO, "version: %s (same version, binary updated)", newVer)
							} else {
								log.Log(log.OK, "update complete!")
								log.Log(log.INFO, "warning: new version %s appears older than current %s", newVer, version.Get())
								log.Log(log.INFO, "this may indicate a downgrade or version mismatch")
							}
						} else {
							log.Log(log.OK, "update complete!")
							log.Log(log.INFO, "new version: %s", outputStr)
						}
					} else {
						log.Log(log.OK, "update complete!")
						log.Log(log.INFO, "new version: %s", outputStr)
					}

					// Check if PATH needs updating
					if installedZapPath == expectedZapPath && originalZapPath != expectedZapPath {
						log.Log(log.INFO, "updated binary installed to: %s", expectedZapPath)
						if !strings.Contains(os.Getenv("PATH"), goBinPath) {
							log.Log(log.INFO, "add %s to your PATH to use the updated version", goBinPath)
						}
					}

					// Warn about shell cache
					if strings.Contains(outputStr, version.Get()) {
						log.Log(log.INFO, "note: version may be cached, restart your terminal or run: hash -r")
					}
				} else {
					log.Log(log.OK, "update complete!")
					log.Log(log.INFO, "could not verify new version (binary may be corrupted)")
					log.Log(log.INFO, "run 'zap version' to verify the new version")
				}
			} else if !originalModTime.IsZero() {
				log.Log(log.OK, "already up to date (version %s)", version.Get())
			} else {
				log.Log(log.OK, "update complete!")
				log.Log(log.INFO, "run 'zap version' to verify the new version")
			}
			return
		}
	}

	// If we can't verify, still report success but warn user
	log.Log(log.OK, "update complete!")
	log.Log(log.INFO, "run 'zap version' to verify the new version")
	if originalZapPath != "" && originalZapPath != expectedZapPath {
		log.Log(log.INFO, "note: binary installed to %s (current: %s)", expectedZapPath, originalZapPath)
		// Check if PATH needs to be configured
		if !strings.Contains(os.Getenv("PATH"), goBinPath) {
			log.Log(log.INFO, "setting up PATH...")
			if err := setupPath(goBinPath); err != nil {
				log.VerboseLog("PATH setup failed: %v", err)
				log.Log(log.INFO, "if version hasn't changed, ensure %s is in your PATH", goBinPath)
			}
		} else {
			log.Log(log.INFO, "if version hasn't changed, ensure %s is in your PATH", goBinPath)
		}
	} else {
		log.Log(log.INFO, "if version hasn't changed, try: hash -r  (or restart your terminal)")
	}
}

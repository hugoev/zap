package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/hugoev/zap/internal/cleanup"
	"github.com/hugoev/zap/internal/config"
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
		handlePorts(cfg, yes, dryRun, jsonOutput, flagValues)
	case "cleanup", "clean":
		handleCleanup(cfg, yes, dryRun, jsonOutput, flagValues)
	case "version", "v":
		if jsonOutput {
			fmt.Printf(`{"version":"%s","commit":"%s","date":"%s"}`+"\n", version.Get(), version.GetCommit(), version.GetDate())
		} else {
			fmt.Printf("zap version %s\n", version.Get())
		}
	case "update":
		handleUpdate()
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

func handlePorts(cfg *config.Config, yes, dryRun, jsonOutput bool, flagValues map[string]string) {
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

	processes, err := ports.ScanPortsRange(portsToScan)
	if err != nil {
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

					if err := ports.KillProcess(proc.PID); err != nil {
						log.Log(log.FAIL, "Failed to kill PID %d: %v", proc.PID, err)
						// Continue with other processes
					} else {
						// Verify it was actually killed
						if !ports.IsProcessRunning(proc.PID) {
							log.Log(log.STOP, "PID %d", proc.PID)
							actualKilledCount++
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

					if err := ports.KillProcess(proc.PID); err != nil {
						log.Log(log.FAIL, "Failed to kill PID %d: %v", proc.PID, err)
						// Continue with other processes
					} else {
						// Verify it was actually killed
						if !ports.IsProcessRunning(proc.PID) {
							log.Log(log.STOP, "PID %d", proc.PID)
							actualKilledCount++
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

func handleUpdate() {
	log.Log(log.SCAN, "checking for updates...")

	// Check if go is available
	goPath, err := exec.LookPath("go")
	if err != nil {
		log.Log(log.FAIL, "go command not found. Please install Go to use the update command.")
		os.Exit(1)
	}
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
		if !strings.Contains(os.Getenv("PATH"), goBinPath) {
			log.Log(log.INFO, "warning: %s is not in your PATH", goBinPath)
			log.Log(log.INFO, "add it to your PATH or the update won't be found")
		}
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

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)

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
			log.VerboseLog("tag fetch attempt %d failed, retrying...", attempt)
			time.Sleep(time.Duration(attempt) * time.Second)
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
		buildCmd := exec.CommandContext(buildCtx, "go", "build", "-ldflags", ldflags, "-o", expectedZapPath, "./cmd/zap")
		buildCmd.Dir = tempDir
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		buildErr := buildCmd.Run()
		buildCancel()

		if buildErr != nil {
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
			os.Chmod(expectedZapPath, 0755)
			log.VerboseLog("built binary with version %s at %s", versionStr, expectedZapPath)
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
	// Give filesystem a moment to sync
	time.Sleep(200 * time.Millisecond)

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
		log.Log(log.INFO, "if version hasn't changed, ensure %s is in your PATH", goBinPath)
	} else {
		log.Log(log.INFO, "if version hasn't changed, try: hash -r  (or restart your terminal)")
	}
}

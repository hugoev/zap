package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/hugoev/zap/internal/cleanup"
	"github.com/hugoev/zap/internal/config"
	"github.com/hugoev/zap/internal/log"
	"github.com/hugoev/zap/internal/ports"
)

const version = "0.2.1"

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
	flags := parseFlags(args)
	yes := flags["yes"] || flags["y"]
	dryRun := flags["dry-run"]
	verbose := flags["verbose"] || flags["v"]

	// Set verbose mode globally
	log.Verbose = verbose

	switch command {
	case "ports", "port":
		handlePorts(cfg, yes, dryRun)
	case "cleanup", "clean":
		handleCleanup(cfg, yes, dryRun)
	case "version", "v":
		fmt.Printf("zap version %s\n", version)
	case "update":
		handleUpdate()
	case "help", "h", "--help", "-h":
		printUsage()
	default:
		log.Log(log.FAIL, "Unknown command: %s", command)
		printUsage()
		os.Exit(1)
	}
}

func parseFlags(args []string) map[string]bool {
	flags := make(map[string]bool)
	for _, arg := range args {
		if strings.HasPrefix(arg, "--") {
			flag := strings.TrimPrefix(arg, "--")
			flags[flag] = true
		} else if strings.HasPrefix(arg, "-") {
			flag := strings.TrimPrefix(arg, "-")
			flags[flag] = true
		}
	}
	return flags
}

func printUsage() {
	fmt.Println("Usage: zap <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  ports     Scan and free up ports")
	fmt.Println("  cleanup   Remove stale dependency/cache folders")
	fmt.Println("  version   Display version information")
	fmt.Println("  update    Update zap to the latest version")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --yes, -y        Execute without confirmation where safe")
	fmt.Println("  --dry-run        Show planned actions without making changes")
	fmt.Println("  --verbose, -v    Show detailed progress and information")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  zap ports         # Free up ports")
	fmt.Println("  zap ports --yes   # Free up ports without prompts")
	fmt.Println("  zap cleanup       # Clean up stale directories")
	fmt.Println("  zap update        # Update to latest version")
}

func handlePorts(cfg *config.Config, yes, dryRun bool) {
	log.Log(log.SCAN, "checking commonly used development ports")
	log.VerboseLog("scanning ports: %v", getCommonPorts())

	processes, err := ports.ScanPorts()
	if err != nil {
		log.Log(log.FAIL, "Failed to scan ports: %v", err)
		os.Exit(1)
	}

	if len(processes) == 0 {
		log.Log(log.OK, "no processes found on common development ports")
		return
	}

	log.VerboseLog("found %d processes on scanned ports", len(processes))

	var safeToKill []ports.ProcessInfo
	var needsConfirmation []ports.ProcessInfo
	var skipped []ports.ProcessInfo

	for _, proc := range processes {
		if cfg.IsPortProtected(proc.Port) {
			log.Log(log.SKIP, ":%d PID %d (%s) protected", proc.Port, proc.PID, proc.Name)
			skipped = append(skipped, proc)
			continue
		}

		// Format process info
		runtimeStr := formatRuntime(proc.Runtime)
		procInfo := fmt.Sprintf(":%d PID %d (%s) [%s]", proc.Port, proc.PID, proc.Name, runtimeStr)
		if log.Verbose {
			cmdPreview := truncateString(proc.Cmd, 60)
			procInfo += fmt.Sprintf(" - %s", cmdPreview)
			if proc.WorkingDir != "" {
				procInfo += fmt.Sprintf(" [%s]", truncateString(proc.WorkingDir, 40))
			}
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
			log.Log(log.ACTION, "terminate %d safe dev server process(es) %v? (y/N): ", len(safeToKill), pids)
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
					if err := ports.KillProcess(proc.PID); err != nil {
						log.Log(log.FAIL, "Failed to kill PID %d: %v", proc.PID, err)
					} else {
						log.Log(log.STOP, "PID %d", proc.PID)
						actualKilledCount++
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
			log.Log(log.ACTION, "terminate %d infrastructure/unknown process(es) %v? (y/N): ", len(needsConfirmation), pids)
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
					if err := ports.KillProcess(proc.PID); err != nil {
						log.Log(log.FAIL, "Failed to kill PID %d: %v", proc.PID, err)
					} else {
						log.Log(log.STOP, "PID %d", proc.PID)
						actualKilledCount++
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

func handleCleanup(cfg *config.Config, yes, dryRun bool) {
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

	for _, scanPath := range scanPaths {
		if _, err := os.Stat(scanPath); os.IsNotExist(err) {
			log.VerboseLog("skipping non-existent path: %s", scanPath)
			continue
		}

		log.VerboseLog("scanning: %s", scanPath)
		progressCallback := func(path string) {
			if log.Verbose {
				log.VerboseLog("  checking: %s", path)
			}
		}

		dirs, err := cleanup.ScanDirectories(scanPath, cfg.ShouldCleanup, progressCallback)
		if err != nil {
			log.VerboseLog("error scanning %s: %v", scanPath, err)
			continue
		}
		allDirs = append(allDirs, dirs...)
		scannedCount++
	}

	log.VerboseLog("scanned %d directory path(s)", scannedCount)

	if len(allDirs) == 0 {
		log.Log(log.OK, "no stale directories found")
		return
	}

	// Display found directories
	totalSize := cleanup.GetTotalSize(allDirs)

	// Sort by size (largest first) for better visibility
	sortedDirs := make([]cleanup.DirectoryInfo, len(allDirs))
	copy(sortedDirs, allDirs)
	for i := 0; i < len(sortedDirs)-1; i++ {
		for j := i + 1; j < len(sortedDirs); j++ {
			if sortedDirs[i].Size < sortedDirs[j].Size {
				sortedDirs[i], sortedDirs[j] = sortedDirs[j], sortedDirs[i]
			}
		}
	}

	log.Log(log.FOUND, "found %d directories (%s total)", len(allDirs), cleanup.FormatSize(totalSize))

	for _, dir := range sortedDirs {
		age := int(time.Since(dir.ModTime).Hours() / 24)
		log.Log(log.FOUND, "%s (%s, %d days old)", dir.Path, cleanup.FormatSize(dir.Size), age)
	}

	shouldDelete := yes
	if !shouldDelete && !dryRun {
		log.Log(log.ACTION, "delete these directories? (y/N): ")
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
			for _, dir := range allDirs {
				if err := cleanup.DeleteDirectory(dir.Path); err != nil {
					log.Log(log.FAIL, "Failed to delete %s: %v", dir.Path, err)
				} else {
					log.Log(log.DELETE, "%s", dir.Path)
					deletedCount++
					freedSize += dir.Size
				}
			}
			log.Log(log.STATS, "deleted %d directories, freed %s", deletedCount, cleanup.FormatSize(freedSize))
		}
	}
}

func confirm() bool {
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
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
	if _, err := exec.LookPath("go"); err != nil {
		log.Log(log.FAIL, "go command not found. Please install Go to use the update command.")
		os.Exit(1)
	}

	// Get current version
	currentVersion := version
	log.Log(log.INFO, "current version: %s", currentVersion)

	// Install latest version
	log.Log(log.INFO, "downloading latest version...")
	cmd := exec.Command("go", "install", "github.com/hugoev/zap/cmd/zap@latest")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Log(log.FAIL, "failed to update: %v", err)
		log.Log(log.INFO, "you can manually update by running: go install github.com/hugoev/zap/cmd/zap@latest")
		os.Exit(1)
	}

	// Try to get the new version by running the updated binary
	log.Log(log.OK, "update complete!")
	log.Log(log.INFO, "run 'zap version' to verify the new version")
}

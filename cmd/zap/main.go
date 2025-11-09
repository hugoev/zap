package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zap/zap/internal/cleanup"
	"github.com/zap/zap/internal/config"
	"github.com/zap/zap/internal/log"
	"github.com/zap/zap/internal/ports"
)

const version = "0.1.0"

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

	switch command {
	case "ports":
		handlePorts(cfg, yes, dryRun)
	case "cleanup":
		handleCleanup(cfg, yes, dryRun)
	case "version":
		fmt.Printf("zap version %s\n", version)
	default:
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
	fmt.Println("  ports     Scan and terminate orphaned dev processes")
	fmt.Println("  cleanup   Remove stale dependency/cache folders")
	fmt.Println("  version   Display version and build metadata")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --yes, -y     Execute without confirmation where safe")
	fmt.Println("  --dry-run     Show planned actions without making changes")
}

func handlePorts(cfg *config.Config, yes, dryRun bool) {
	log.Log(log.SCAN, "checking commonly used development ports")

	processes, err := ports.ScanPorts()
	if err != nil {
		log.Log(log.FAIL, "Failed to scan ports: %v", err)
		os.Exit(1)
	}

	if len(processes) == 0 {
		log.Log(log.OK, "no processes found on common development ports")
		return
	}

	var safeToKill []ports.ProcessInfo
	var needsConfirmation []ports.ProcessInfo
	var skipped []ports.ProcessInfo

	for _, proc := range processes {
		if cfg.IsPortProtected(proc.Port) {
			log.Log(log.SKIP, ":%d PID %d (%s) protected", proc.Port, proc.PID, proc.Name)
			skipped = append(skipped, proc)
			continue
		}

		if ports.IsInfrastructureProcess(proc) {
			needsConfirmation = append(needsConfirmation, proc)
			log.Log(log.FOUND, ":%d PID %d (%s)", proc.Port, proc.PID, proc.Name)
		} else if ports.IsSafeDevServer(proc) {
			safeToKill = append(safeToKill, proc)
			log.Log(log.FOUND, ":%d PID %d (%s)", proc.Port, proc.PID, proc.Name)
		} else {
			needsConfirmation = append(needsConfirmation, proc)
			log.Log(log.FOUND, ":%d PID %d (%s)", proc.Port, proc.PID, proc.Name)
		}
	}

	// Kill safe processes
	if len(safeToKill) > 0 {
		pids := make([]int, len(safeToKill))
		for i, proc := range safeToKill {
			pids[i] = proc.PID
		}

		shouldKill := yes || cfg.AutoConfirmSafeActions
		if !shouldKill && !dryRun {
			log.Log(log.ACTION, "terminate processes %v? (y/N): ", pids)
			shouldKill = confirm()
		}

		if shouldKill {
			if dryRun {
				for _, proc := range safeToKill {
					log.Log(log.STOP, "PID %d (would terminate)", proc.PID)
				}
			} else {
				for _, proc := range safeToKill {
					if err := ports.KillProcess(proc.PID); err != nil {
						log.Log(log.FAIL, "Failed to kill PID %d: %v", proc.PID, err)
					} else {
						log.Log(log.STOP, "PID %d", proc.PID)
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
			log.Log(log.ACTION, "terminate infrastructure processes %v? (y/N): ", pids)
			shouldKill = confirm()
		}

		if shouldKill {
			if dryRun {
				for _, proc := range needsConfirmation {
					log.Log(log.STOP, "PID %d (would terminate)", proc.PID)
				}
			} else {
				for _, proc := range needsConfirmation {
					if err := ports.KillProcess(proc.PID); err != nil {
						log.Log(log.FAIL, "Failed to kill PID %d: %v", proc.PID, err)
					} else {
						log.Log(log.STOP, "PID %d", proc.PID)
					}
				}
			}
		}
	}

	if !dryRun {
		log.Log(log.OK, "ports freed")
	}
}

func handleCleanup(cfg *config.Config, yes, dryRun bool) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Log(log.FAIL, "Failed to get home directory: %v", err)
		os.Exit(1)
	}

	// Scan common development directories
	scanPaths := []string{
		filepath.Join(homeDir, "Documents"),
		filepath.Join(homeDir, "Projects"),
		filepath.Join(homeDir, "Code"),
		filepath.Join(homeDir, "workspace"),
		filepath.Join(homeDir, "work"),
	}

	var allDirs []cleanup.DirectoryInfo
	for _, scanPath := range scanPaths {
		if _, err := os.Stat(scanPath); os.IsNotExist(err) {
			continue
		}

		dirs, err := cleanup.ScanDirectories(scanPath, cfg.ShouldCleanup)
		if err != nil {
			continue
		}
		allDirs = append(allDirs, dirs...)
	}

	if len(allDirs) == 0 {
		log.Log(log.OK, "no stale directories found")
		return
	}

	// Display found directories
	totalSize := cleanup.GetTotalSize(allDirs)
	log.Log(log.FOUND, "found %d directories (%s total)", len(allDirs), cleanup.FormatSize(totalSize))

	for _, dir := range allDirs {
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
			for _, dir := range allDirs {
				log.Log(log.DELETE, "%s (would delete)", dir.Path)
			}
		} else {
			for _, dir := range allDirs {
				if err := cleanup.DeleteDirectory(dir.Path); err != nil {
					log.Log(log.FAIL, "Failed to delete %s: %v", dir.Path, err)
				} else {
					log.Log(log.DELETE, "%s", dir.Path)
				}
			}
			log.Log(log.OK, "cleanup complete, freed %s", cleanup.FormatSize(totalSize))
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

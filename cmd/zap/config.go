package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hugoev/zap/internal/config"
	"github.com/hugoev/zap/internal/log"
)

func handleConfig(cfg *config.Config, args []string) {
	if len(args) == 0 {
		// Show current config
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			log.Log(log.FAIL, "Failed to serialize config: %v", err)
			os.Exit(1)
		}
		fmt.Println(string(data))
		return
	}

	subcommand := args[0]
	switch subcommand {
	case "show":
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			log.Log(log.FAIL, "Failed to serialize config: %v", err)
			os.Exit(1)
		}
		fmt.Println(string(data))

	case "set":
		if len(args) < 3 {
			log.Log(log.FAIL, "Usage: zap config set <key> <value>")
			log.Log(log.INFO, "Keys: protected_ports, max_age_days, exclude_path, auto_confirm")
			os.Exit(1)
		}
		key := args[1]
		value := args[2]

		switch key {
		case "protected_ports":
			ports := strings.Split(value, ",")
			var portList []int
			for _, p := range ports {
				port, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil {
					log.Log(log.FAIL, "Invalid port: %s", p)
					os.Exit(1)
				}
				portList = append(portList, port)
			}
			cfg.ProtectedPorts = portList
			if err := config.Save(cfg); err != nil {
				log.Log(log.FAIL, "Failed to save config: %v", err)
				os.Exit(1)
			}
			log.Log(log.OK, "Updated protected ports: %v", portList)

		case "max_age_days":
			days, err := strconv.Atoi(value)
			if err != nil {
				log.Log(log.FAIL, "Invalid number of days: %s", value)
				os.Exit(1)
			}
			if days < 1 || days > 365 {
				log.Log(log.FAIL, "Days must be between 1 and 365")
				os.Exit(1)
			}
			cfg.MaxAgeDaysForCleanup = days
			if err := config.Save(cfg); err != nil {
				log.Log(log.FAIL, "Failed to save config: %v", err)
				os.Exit(1)
			}
			log.Log(log.OK, "Updated max age for cleanup: %d days", days)

		case "exclude_path":
			if err := cfg.AddExcludePath(value); err != nil {
				log.Log(log.FAIL, "Failed to add exclude path: %v", err)
				os.Exit(1)
			}
			log.Log(log.OK, "Added exclude path: %s", value)

		case "auto_confirm":
			autoConfirm := value == "true" || value == "1" || value == "yes"
			cfg.AutoConfirmSafeActions = autoConfirm
			if err := config.Save(cfg); err != nil {
				log.Log(log.FAIL, "Failed to save config: %v", err)
				os.Exit(1)
			}
			log.Log(log.OK, "Updated auto_confirm_safe_actions: %v", autoConfirm)

		default:
			log.Log(log.FAIL, "Unknown config key: %s", key)
			log.Log(log.INFO, "Available keys: protected_ports, max_age_days, exclude_path, auto_confirm")
			os.Exit(1)
		}

	case "reset":
		*cfg = config.Config{
			ProtectedPorts:         []int{5432, 6379, 3306, 27017},
			MaxAgeDaysForCleanup:   14,
			ExcludePaths:           []string{},
			AutoConfirmSafeActions: false,
		}
		if err := config.Save(cfg); err != nil {
			log.Log(log.FAIL, "Failed to save config: %v", err)
			os.Exit(1)
		}
		log.Log(log.OK, "Reset configuration to defaults")

	default:
		log.Log(log.FAIL, "Unknown config command: %s", subcommand)
		log.Log(log.INFO, "Available commands: show, set, reset")
		os.Exit(1)
	}
}


package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Load or create config
	config, err := loadConfig()
	if err != nil {
		// First run - prompt for directory
		dir, err := promptForClaudeDir()
		if err != nil {
			fmt.Printf("Error reading input: %v\n", err)
			os.Exit(1)
		}

		// Validate directory exists
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			fmt.Printf("Error: Directory does not exist: %s\n", dir)
			fmt.Println("Please create the directory or specify a different path.")
			os.Exit(1)
		}

		config = &Config{
			ClaudeDir: dir,
		}
		if err := saveConfig(config); err != nil {
			fmt.Printf("Warning: Could not save config: %v\n", err)
		} else {
			fmt.Printf("\n✓ Configuration saved to: %s\n\n", configPath)
		}
	}

	// Initialize paths from config
	initializePaths(config.ClaudeDir)

	// Run TUI
	p := tea.NewProgram(initialModel(config), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

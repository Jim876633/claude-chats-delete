package main

import (
	"fmt"
	"os"
	"os/exec"

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
	finalModel, err := p.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// If the user chose to resume a chat, hand off to `claude -r` in that
	// chat's project directory after the TUI has torn down the alt screen.
	if m, ok := finalModel.(model); ok && m.resumeUUID != "" {
		resumeChat(m.resumeDir, m.resumeUUID)
	}
}

// resumeChat launches `claude -r <uuid>` with the working directory set to the
// chat's project folder, replacing the TUI with an interactive Claude session.
func resumeChat(dir, uuid string) {
	cmd := exec.Command("claude", "-r", uuid)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Could not launch claude: %v\n", err)
		fmt.Printf("Resume manually with:\n  cd %s && claude -r %s\n", dir, uuid)
	}
}

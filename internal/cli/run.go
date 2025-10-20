package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/iambrandonn/lorch/internal/config"
	"github.com/iambrandonn/lorch/internal/workspace"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start a new orchestration run",
	Long: `Start a new orchestration run. If --task is not specified,
lorch will prompt for natural language instructions (Phase 2).`,
	RunE: runRun,
}

func init() {
	runCmd.Flags().StringP("task", "t", "", "Task ID to execute (e.g., T-0042)")
}

func runRun(cmd *cobra.Command, args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Find or create config
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}

	cfg, cfgPath, err := loadOrCreateConfig(configPath, logger)
	if err != nil {
		return err
	}

	logger.Info("loaded configuration", "path", cfgPath)

	// Validate config
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Determine workspace root (relative to config file location)
	workspaceRoot := determineWorkspaceRoot(cfg, cfgPath)
	logger.Info("workspace root", "path", workspaceRoot)

	// Initialize workspace directories
	if err := workspace.Initialize(workspaceRoot); err != nil {
		return fmt.Errorf("failed to initialize workspace: %w", err)
	}

	logger.Info("workspace initialized")

	// Get task ID if specified
	taskID, err := cmd.Flags().GetString("task")
	if err != nil {
		return err
	}

	if taskID != "" {
		logger.Info("task specified", "task_id", taskID)
		// Phase 1.1: We just validate the setup for now
		// Actual execution will come in later milestones
		logger.Info("Phase 1.1: setup complete, execution not yet implemented")
	} else {
		// Phase 2: Natural language intake
		logger.Info("Phase 2: natural language intake not yet implemented")
	}

	return nil
}

// loadOrCreateConfig finds an existing config or creates a new one
// Following the decision: walk up directory tree, create in CWD if not found
func loadOrCreateConfig(configPath string, logger *slog.Logger) (*config.Config, string, error) {
	// If explicit path provided, use it
	if configPath != "" {
		cfg, err := config.LoadFromFile(configPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load config from %s: %w", configPath, err)
		}
		return cfg, configPath, nil
	}

	// Search up directory tree for lorch.json
	foundPath, err := findConfigInTree()
	if err != nil {
		return nil, "", err
	}

	if foundPath != "" {
		logger.Info("found existing config", "path", foundPath)
		cfg, err := config.LoadFromFile(foundPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load config: %w", err)
		}
		return cfg, foundPath, nil
	}

	// No config found, create default in current directory
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get current directory: %w", err)
	}

	defaultPath := filepath.Join(cwd, "lorch.json")
	logger.Info("no config found, creating default", "path", defaultPath)

	cfg := config.GenerateDefault()
	if err := cfg.SaveToFile(defaultPath); err != nil {
		return nil, "", fmt.Errorf("failed to save default config: %w", err)
	}

	logger.Info("created default config", "path", defaultPath)
	return cfg, defaultPath, nil
}

// findConfigInTree searches up the directory tree for lorch.json
func findConfigInTree() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	for {
		configPath := filepath.Join(dir, "lorch.json")
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root
			break
		}
		dir = parent
	}

	return "", nil
}

// determineWorkspaceRoot resolves the workspace root relative to the config file
// Following the decision: resolve relative to directory containing lorch.json
func determineWorkspaceRoot(cfg *config.Config, configPath string) string {
	configDir := filepath.Dir(configPath)
	if cfg.WorkspaceRoot == "." {
		return configDir
	}
	return filepath.Join(configDir, cfg.WorkspaceRoot)
}

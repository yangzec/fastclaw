package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/fastclaw-ai/fastclaw/internal/config"
	"github.com/fastclaw-ai/fastclaw/internal/plugin"
)

const hubRepo = "fastclaw-ai/fastclaw"

// pluginCmd handles plugin management subcommands.
func pluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "plugins",
		Aliases: []string{"plugin"},
		Short:   "Manage plugins",
	}
	cmd.AddCommand(pluginListCmd())
	cmd.AddCommand(pluginInstallCmd())
	cmd.AddCommand(pluginRemoveCmd())
	return cmd
}

func pluginListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List discovered plugins and their status",
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, err := config.HomeDir()
			if err != nil {
				return err
			}

			paths := []string{filepath.Join(homeDir, "plugins")}

			mgr := plugin.NewManager(nil)
			if err := mgr.Discover(paths); err != nil {
				return err
			}

			plugins := mgr.Plugins()
			if len(plugins) == 0 {
				fmt.Println("No plugins found.")
				fmt.Println("Plugin directories:", paths)
				return nil
			}

			fmt.Printf("%-15s %-20s %-10s %-10s %s\n", "ID", "NAME", "TYPE", "VERSION", "DIR")
			for _, p := range plugins {
				enabledStr := "enabled"
				fmt.Printf("%-15s %-20s %-10s %-10s %s [%s]\n",
					p.Manifest.ID,
					p.Manifest.Name,
					p.Manifest.Type,
					p.Manifest.Version,
					p.Manifest.Dir,
					enabledStr,
				)
			}
			return nil
		},
	}
}

func pluginInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <name|github-url|npm-package|path>",
		Short: "Install a plugin from FastClaw Hub, GitHub, npm, or local path",
		Long: `Install a plugin. The source is auto-detected:

  fastclaw plugins install telegram                        # FastClaw Hub
  fastclaw plugins install github.com/user/repo            # GitHub repo
  fastclaw plugins install @ollama/web-search              # npm plugin (bridged)
  fastclaw plugins install ./my-plugin                     # local directory`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]

			homeDir, err := config.HomeDir()
			if err != nil {
				return err
			}
			pluginsDir := filepath.Join(homeDir, "plugins")

			res, err := plugin.Install(source, pluginsDir)
			if err != nil {
				return err
			}
			fmt.Printf("Plugin %q installed to %s\n", res.ID, res.Path)
			if res.NeedsRestart {
				fmt.Println("Restart FastClaw so the new plugin is loaded.")
			}
			return nil
		},
	}
}

func pluginRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove an installed plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			homeDir, err := config.HomeDir()
			if err != nil {
				return err
			}

			pluginDir := filepath.Join(homeDir, "plugins", id)
			if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
				return fmt.Errorf("plugin %q not found at %s", id, pluginDir)
			}

			if err := os.RemoveAll(pluginDir); err != nil {
				return fmt.Errorf("remove plugin: %w", err)
			}

			fmt.Printf("Plugin %q removed.\n", id)
			return nil
		},
	}
}

// Command unagit is a GitLab TUI for cloning projects and reviewing merge
// requests.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/ui"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "unagit",
		Short: "GitLab TUI for cloning projects and reviewing merge requests",
		Long: "unagit lists the projects and open merge requests of the GitLab groups you\n" +
			"selected, clones them under a root directory and opens your editor there.\n\n" +
			"Everything is configured from the Settings tab: the servers, their tokens,\n" +
			"the groups and where each of them is cloned to. Tokens are encrypted with a\n" +
			"passphrase (Argon2id + AES-256-GCM) that is asked for in a dialog on every\n" +
			"start; they only ever exist in memory.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			return ui.NewLocked(cfg).Run()
		},
	}
	root.AddCommand(whereCmd())
	return root
}

func whereCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "where",
		Short: "Print the configuration paths",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("config:  ", config.Path())
			fmt.Println("tokens:  ", config.VaultPath())
			fmt.Println("projects:", config.IndexPath("projects"))
			fmt.Println("mrs:     ", config.IndexPath("mrs"))
			fmt.Println("groups:  ", config.IndexPath("groups"))
		},
	}
}

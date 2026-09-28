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
	root.AddCommand(cdCmd(), sessionsCmd(), whereCmd(),
		goalCmd("review", "Open a merge request for review, from its link", true),
		goalCmd("open", "Open a merge request's branch worktree, from its link", false))
	return root
}

// goalCmd starts the TUI with a merge request to open straight away: the
// passphrase is still asked for in its dialog, and when the editor closes the
// lists are there as usual. The link is read before anything starts, so a
// mistyped one is an error on the command line rather than inside the TUI.
func goalCmd(use, short string, review bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <link>",
		Short: short,
		Long: short + ".\n\nThe link is the merge request's page as the browser shows it, e.g.\n" +
			"  https://gitlab.example.com/group/project/-/merge_requests/12\n" +
			"  https://github.com/owner/repo/pull/12\n" +
			"Anything after the number (a tab, a comment anchor) is ignored.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			link, err := ui.ParseMRLink(cfg, args[0])
			if err != nil {
				return err
			}
			return ui.NewLocked(cfg).WithGoal(ui.Goal{Link: link, Review: review}).Run()
		},
	}
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

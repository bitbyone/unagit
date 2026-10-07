package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/ui"
	"github.com/tobola/unagit/internal/zoxide"
)

func goCmd() *cobra.Command {
	var print bool
	cmd := &cobra.Command{
		Use:   "go [search]",
		Short: "Open a shell in a repository or worktree unagit has on disk",
		Long: "go starts a shell in a clone, a merge request's worktree, another worktree or\n" +
			"a grouped worktree - anything unagit has on disk - without the interface:\n\n" +
			"    unagit go\n" +
			"    unagit go gateway\n" +
			"    unagit go 'gateway !7'\n\n" +
			"The search is fuzzy, over the repository, the merge request and its title,\n" +
			"the branch and the folder. One match is entered without asking; more are\n" +
			"listed best first to choose from, and / narrows the list further. Leave\n" +
			"the shell and you are back where you were.\n\n" +
			"No passphrase is asked for: the lists are read from unagit's own index and\n" +
			"the disk, so what was cloned since the last refresh shows once unagit has\n" +
			"seen it.\n\n" +
			"--print writes the directory to standard output instead:\n\n" +
			"    ugo() { cd \"$(unagit go --print \"$@\")\" || return; }",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			places := ui.Places(cfg)
			if len(places) == 0 {
				return fmt.Errorf("nothing is on disk yet - clone something from unagit first")
			}
			if query := strings.Join(args, " "); query != "" {
				places = ui.MatchPlaces(places, query)
				if len(places) == 0 {
					return fmt.Errorf("nothing on disk matches %q", query)
				}
			}
			chosen := places[0]
			if len(places) > 1 {
				picked, ok, err := ui.PickPlace(places)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("nothing chosen")
				}
				chosen = picked
			}
			if tool := zoxide.New(); tool.Enabled(cfg.Integrations.Zoxide) {
				_ = tool.Add(chosen.Dir)
			}
			if print {
				fmt.Println(chosen.Dir)
				return nil
			}
			return enter(chosen)
		},
	}
	cmd.Flags().BoolVarP(&print, "print", "p", false, "print the directory instead of starting a shell there")
	return cmd
}

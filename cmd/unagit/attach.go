package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/ui"
	"github.com/tobola/unagit/internal/zoxide"
)

func attachCmd() *cobra.Command {
	return &cobra.Command{
		Use: "attach [query]", Short: "Return to a running Neovim, including one left by another unagit",
		Args: cobra.ArbitraryArgs, SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, readable := sessionsConfig()
			open := session.New(cfg.Dir()).Running()
			if query := strings.Join(args, " "); query != "" {
				open = ui.MatchPlaces(open, query)
			}
			if len(open) == 0 {
				return fmt.Errorf("no running editor matches; open Neovim in unagit and use Ctrl-Z to put it aside")
			}
			chosen := open[0]
			if len(open) > 1 {
				r, ok, err := ui.PickSession(open)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("nothing chosen")
				}
				chosen = r
			}
			if !editors.SocketAlive(chosen.Socket) {
				return fmt.Errorf("editor has closed; open the directory again")
			}
			remote := editors.AttachCommand(chosen.Launcher, chosen.Socket, chosen.Dir)
			if err := os.Chdir(chosen.Dir); err != nil {
				return err
			}
			if tool := zoxide.New(); readable && tool.Enabled(cfg.Integrations.Zoxide) {
				_ = tool.Add(chosen.Dir)
			}
			return syscall.Exec(remote.Path, remote.Args, environ(map[string]string{"PWD": chosen.Dir}))
		},
	}
}

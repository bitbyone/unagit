package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/credential"
)

// credentialCmd is what a sandbox's runtime runs on the host for a token
// unagit keeps: a client of the running unagit's socket, with no vault
// and no question of its own, since sbx runs it with no terminal.
func credentialCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "credential",
		Short: "Hand a token kept for unibox's sandboxes to their runtime",
	}
	var socket string
	get := &cobra.Command{
		Use:   "get <name>",
		Short: "Print the sandbox token stored under name, from the running, unlocked unagit",
		Long: "Print the sandbox token stored under name, from the running, unlocked unagit.\n\n" +
			"A unibox profile names this command as an agent's or a service's token source;\n" +
			"the runtime runs it on the host whenever it wants the token. It asks for\n" +
			"nothing: while unagit is closed or locked it fails and says so.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if socket == "" {
				socket = credential.SocketPath(config.Dir())
			}
			token, err := credential.Get(socket, args[0])
			if err != nil {
				return err
			}
			// The token alone: the runtime stores what is printed.
			_, err = fmt.Fprint(os.Stdout, token)
			return err
		},
	}
	get.Flags().StringVar(&socket, "socket", "", "the socket of the unagit to ask (default: the one of its configuration directory)")
	c.AddCommand(get)
	return c
}

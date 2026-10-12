package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tobola/unagit/internal/atbox/api"
)

func (o *options) herdrCommand() *cobra.Command {
	c := &cobra.Command{Use: "herdr", Short: "Install and operate the sandbox agent status plugin"}
	c.AddCommand(&cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return o.write(map[string]any{"api_version": api.Version, "plugin": o.manager.PluginStatus(cmd.Context())})
	}})
	c.AddCommand(&cobra.Command{Use: "install-plugin", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := o.confirm("Install the atbox herdr plugin and its background observer?"); err != nil {
			return err
		}
		if err := o.manager.InstallPlugin(cmd.Context()); err != nil {
			return err
		}
		return o.success("herdr plugin installed")
	}})
	c.AddCommand(&cobra.Command{Use: "uninstall-plugin", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := o.confirm("Uninstall the atbox herdr plugin?"); err != nil {
			return err
		}
		if err := o.manager.UninstallPlugin(cmd.Context()); err != nil {
			return err
		}
		return o.success("herdr plugin uninstalled")
	}})
	c.AddCommand(&cobra.Command{Use: "ensure", Hidden: true, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error { return o.manager.EnsureObserver() }})
	c.AddCommand(&cobra.Command{Use: "observe", Hidden: true, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return o.manager.Observe(ctx)
	}})
	return c
}

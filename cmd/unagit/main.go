// Command unagit is a GitLab TUI for cloning projects and reviewing merge
// requests.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/platform"
	"github.com/tobola/unagit/internal/ui"
	"github.com/tobola/unagit/internal/version"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// debug is --debug, for whichever command starts the interface.
var debug bool

// standalone is -s: the repository of a directory alone, no server.
var standalone bool

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "unagit [-s [DIR]]",
		Short: "GitLab TUI for cloning projects and reviewing merge requests",
		Long: "unagit lists the projects and open merge requests of the GitLab groups you\n" +
			"selected, clones them under a root directory and opens your editor there.\n\n" +
			"Everything is configured from the Settings tab: the servers, their tokens,\n" +
			"the groups and where each of them is cloned to. Tokens are encrypted with a\n" +
			"passphrase (Argon2id + AES-256-GCM) that is asked for in a dialog on every\n" +
			"start; they only ever exist in memory.",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if !standalone && len(args) > 0 {
				return fmt.Errorf("unknown command %q for unagit - for one repository alone, unagit -s %s", args[0], args[0])
			}
			return cobra.MaximumNArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if standalone {
				return runStandalone(args)
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			return ui.NewLocked(cfg).WithDebug(debug).Run()
		},
	}
	root.Flags().BoolVarP(&standalone, "standalone", "s", false,
		"work on the repository of DIR (default .) alone: its changes, log, branches and shelf, no server and no passphrase")
	root.PersistentFlags().BoolVar(&debug, "debug", false, "list Settings › Debug, which fires toasts and notifications by hand")
	// --version prints the version alone, the same line as unagit version.
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(attachCmd(), cdCmd(), credentialCmd(), goCmd(), sessionsCmd(), whereCmd(), launchCmd(), holdCmd(), versionCmd(),
		goalCmd("review", "Open a merge request for review, from its link", true),
		goalCmd("open", "Open a merge request's branch worktree, from its link", false))
	return root
}

// runStandalone starts unagit on one repository. The configuration is
// only read - the theme, the editors - and nothing of unagit's is written.
func runStandalone(args []string) error {
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	cfg, err := config.LoadReadOnly()
	if err != nil {
		return err
	}
	app, err := ui.NewStandalone(cfg, dir)
	if err != nil {
		return err
	}
	return app.WithDebug(debug).Run()
}

// launchCmd is what herdr and Ghostty run in a pane unagit opens: they take a
// line of text, so the command itself comes in the environment (mux).
func launchCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "launch",
		Short:  "Run what unagit opened in a pane",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   func(*cobra.Command, []string) error { return mux.RunLaunch() },
	}
}

// holdCmd runs a program and, when it fails, waits for Enter before the
// terminal is unagit's again, so what it said can be read: what sh does
// for unibox elsewhere, for Windows, which has no sh.
func holdCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "hold -- program [args...]",
		Short:              "Run a program, waiting for Enter when it fails",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}
			if len(args) == 0 {
				return fmt.Errorf("unagit hold runs a program unagit gives it; it is not for typing")
			}
			os.Exit(hold(args, os.Stdin, os.Stdout))
			return nil
		},
	}
}

// hold runs args with the terminal and, when the program fails other than
// by being interrupted, says so and waits for a line before handing back
// its exit code.
func hold(args []string, in io.Reader, out io.Writer) int {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	platform.IgnoreInterrupts()
	code := 0
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			fmt.Fprintf(out, "\n%s could not start: %v - press Enter to go back\n", filepath.Base(args[0]), err)
			_, _ = bufio.NewReader(in).ReadString('\n')
			return 1
		}
		code = exit.ExitCode()
	}
	// 130 is an interrupt, which the person asked for and has seen.
	if code != 0 && code != 130 {
		fmt.Fprintf(out, "\n%s stopped (exit %d) - press Enter to go back\n", filepath.Base(args[0]), code)
		_, _ = bufio.NewReader(in).ReadString('\n')
	}
	return code
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
			return ui.NewLocked(cfg).WithGoal(ui.Goal{Link: link, Review: review}).WithDebug(debug).Run()
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

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of unagit",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version.String())
		},
	}
}

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/ui"
	"github.com/tobola/unagit/internal/zoxide"
)

func cdCmd() *cobra.Command {
	var print bool
	cmd := &cobra.Command{
		Use:   "cd [search]",
		Short: "Open a shell in the directory of a merge request open in an editor",
		Long: "cd starts a shell in the directory unagit currently has open in an editor,\n" +
			"so another terminal can follow it there. Leave the shell and you are back\n" +
			"where you were:\n\n" +
			"    unagit cd\n" +
			"    unagit cd calling\n\n" +
			"A process cannot change the directory of the shell that started it, so this\n" +
			"is a shell of its own - the same way chezmoi cd works. $SHELL is what runs.\n\n" +
			"With more than one thing open it asks which; a search narrows it first, and\n" +
			"when only one thing matches it is entered without asking.\n\n" +
			"--print writes the directory to standard output instead, for scripts and\n" +
			"for changing the directory of the current shell:\n\n" +
			"    ug() { cd \"$(unagit cd --print \"$@\")\" || return; }",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, readable := sessionsConfig()
			open := session.New(cfg.Dir()).List()
			if len(open) == 0 {
				return fmt.Errorf("nothing is open in an editor right now")
			}
			if query := strings.Join(args, " "); query != "" {
				open = matching(open, query)
				if len(open) == 0 {
					return fmt.Errorf("nothing open matches %q", query)
				}
			}
			chosen := open[0]
			if len(open) > 1 {
				picked, ok, err := ui.PickSession(open)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("nothing chosen")
				}
				chosen = picked
			}
			if tool := zoxide.New(); readable && tool.Enabled(cfg.Integrations.Zoxide) {
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

// enter replaces unagit with a shell in the directory. Replacing rather than
// spawning keeps the process tree flat, hands job control straight to the
// shell and lets its exit status be ours.
func enter(r session.Record) error {
	from, _ := os.Getwd()
	if err := os.Chdir(r.Dir); err != nil {
		return fmt.Errorf("%s: %w", r.Dir, err)
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	if !filepath.IsAbs(sh) {
		found, err := exec.LookPath(sh)
		if err != nil {
			return fmt.Errorf("finding the shell %q: %w", sh, err)
		}
		sh = found
	}
	fmt.Fprintf(os.Stderr, "%s · %s — exit to come back\n", r.Label(), r.Dir)

	// A login shell would start somewhere else, so it is a plain interactive
	// one. PWD is inherited and would otherwise still name the directory we
	// were called from; OLDPWD makes "cd -" go back there, and UNAGIT_CD lets
	// a prompt say what this shell is.
	env := environ(map[string]string{
		"PWD":       r.Dir,
		"OLDPWD":    from,
		"UNAGIT_CD": r.Dir,
	})
	return syscall.Exec(sh, []string{filepath.Base(sh)}, env)
}

// environ is the environment with those names set to those values, replacing
// any the parent already had.
func environ(set map[string]string) []string {
	out := make([]string, 0, len(os.Environ())+len(set))
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, replaced := set[name]; !replaced {
			out = append(out, kv)
		}
	}
	for name, value := range set {
		if value != "" {
			out = append(out, name+"="+value)
		}
	}
	return out
}

func sessionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List what unagit has open in an editor",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			open := session.New(config.Dir()).List()
			if len(open) == 0 {
				fmt.Fprintln(os.Stderr, "nothing is open in an editor right now")
				return
			}
			for _, r := range open {
				fmt.Printf("%s\t%s\t%s\n", r.Label(), r.Mode, r.Dir)
			}
		},
	}
}

// matching narrows the list to what the search mentions.
func matching(open []session.Record, query string) []session.Record {
	query = strings.ToLower(query)
	var out []session.Record
	for _, r := range open {
		hay := strings.ToLower(fmt.Sprintf("%s %s %d %s %s", r.Project, r.Title, r.IID, r.Mode, r.Server))
		if strings.Contains(hay, query) {
			out = append(out, r)
		}
	}
	return out
}

// sessionsConfig is the configuration of a command that needs only where
// the sessions are and whether zoxide is wanted. One that cannot be read
// must not keep a shell from a directory open in an editor, so the sessions
// are read where they always are, and readable is false: zoxide is then
// left alone, since its switch is not known.
func sessionsConfig() (cfg *config.Config, readable bool) {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
		cfg.SetDir(config.Dir())
		return cfg, false
	}
	return cfg, true
}

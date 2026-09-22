package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/spf13/cobra"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/session"
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
			open := session.New(config.Dir()).List()
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
				picked, err := pick(open)
				if err != nil {
					return err
				}
				if picked.Dir == "" {
					return fmt.Errorf("nothing chosen")
				}
				chosen = picked
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

// pick asks which one, drawing on the terminal itself. tcell talks to
// /dev/tty rather than to standard output, which is what keeps --print usable
// in a command substitution.
func pick(open []session.Record) (session.Record, error) {
	app := tview.NewApplication()
	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorder(true).
		SetTitle(" Open in an editor ").
		SetTitleAlign(tview.AlignLeft)

	var chosen session.Record
	for _, r := range open {
		r := r
		what := r.Mode
		if r.Title != "" {
			what += " · " + r.Title
		}
		list.AddItem(r.Label(), "  "+what+"  ·  "+r.Dir, 0, func() {
			chosen = r
			app.Stop()
		})
	}
	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch {
		case ev.Key() == tcell.KeyEsc, ev.Rune() == 'q':
			app.Stop()
			return nil
		case ev.Rune() == 'j':
			return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
		case ev.Rune() == 'k':
			return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
		}
		return ev
	})

	if err := app.SetRoot(list, true).Run(); err != nil {
		return session.Record{}, err
	}
	return chosen, nil
}

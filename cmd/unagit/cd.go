package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/spf13/cobra"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/session"
)

func cdCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cd [search]",
		Short: "Print the directory of a merge request open in an editor",
		Long: "cd prints the directory unagit currently has open in an editor, so another\n" +
			"terminal can follow it there:\n\n" +
			"    cd \"$(unagit cd)\"\n\n" +
			"With more than one open it asks which, drawing on the terminal and leaving\n" +
			"standard output for the directory alone. A search narrows it first, and when\n" +
			"only one thing matches it is printed without asking.\n\n" +
			"Worth putting in your shell:\n\n" +
			"    ug() { cd \"$(unagit cd \"$@\")\" || return; }",
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
			if len(open) == 1 {
				fmt.Println(open[0].Dir)
				return nil
			}
			chosen, err := pick(open)
			if err != nil {
				return err
			}
			if chosen == "" {
				return fmt.Errorf("nothing chosen")
			}
			fmt.Println(chosen)
			return nil
		},
	}
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
// /dev/tty rather than to standard output, which is what keeps the chosen
// directory usable in a command substitution.
func pick(open []session.Record) (string, error) {
	app := tview.NewApplication()
	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorder(true).
		SetTitle(" Open in an editor ").
		SetTitleAlign(tview.AlignLeft)

	chosen := ""
	for _, r := range open {
		r := r
		what := r.Mode
		if r.Title != "" {
			what += " · " + r.Title
		}
		list.AddItem(r.Label(), "  "+what+"  ·  "+r.Dir, 0, func() {
			chosen = r.Dir
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
		return "", err
	}
	return chosen, nil
}

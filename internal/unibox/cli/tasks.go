package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tobola/unagit/internal/unibox"
	"github.com/tobola/unagit/internal/unibox/api"
)

// Cobra calls a flag's Value in argument order. Keeping that order here makes
// one leading branch a default and a later branch belong only to its repository.
type orderedValue struct {
	req    *unibox.Request
	branch bool
}

func (v *orderedValue) String() string { return "" }
func (v *orderedValue) Type() string   { return "string" }
func (v *orderedValue) Set(value string) error {
	if v.branch {
		if len(v.req.Repositories) == 0 {
			v.req.Branch = value
		} else {
			v.req.Repositories[len(v.req.Repositories)-1].Branch = value
		}
		return nil
	}
	in := unibox.RepoInput{Source: value}
	if name, source, ok := strings.Cut(value, "="); ok && unibox.ValidName(name) == nil {
		in.Name = name
		in.Source = source
	}
	v.req.Repositories = append(v.req.Repositories, in)
	return nil
}

func taskFlags(c *cobra.Command, r *unibox.Request) {
	f := c.Flags()
	f.StringVar(&r.Sandbox, "sandbox", "", "Existing sandbox name")
	f.StringVar(&r.NewSandbox, "new-sandbox", "", "Create a sandbox with this name")
	f.StringVar(&r.Profile, "profile", "", "Environment profile for a new sandbox")
	f.StringVar(&r.Runtime, "runtime", "", "Runtime adapter (existing sandboxes keep theirs)")
	f.StringVar(&r.Agent, "agent", "", "Coding agent declared by the profile")
	f.StringVar(&r.Task, "task", "", "Task name to prepare or resume")
	f.StringVar(&r.Author, "author", "", "Commit author, as Name <email>")
	f.StringSliceVar(&r.Instructions, "instructions", nil, "Named instruction texts for a new sandbox")
	f.BoolVar(&r.Push, "push", false, "Push selected local branches; create one nobody has from the local HEAD, or a URL's default tip (never force)")
	f.BoolVar(&r.IgnoreLocal, "ignore-local", false, "Use the forge version despite uncommitted or unpushed local changes")
	f.Var(&orderedValue{req: r}, "repo", "Repository path or HTTPS URL, optionally name=source (repeatable)")
	f.Var(&orderedValue{req: r, branch: true}, "branch", "Before all repos: shared branch; after a repo: override only that repo")
	c.MarkFlagsMutuallyExclusive("sandbox", "new-sandbox")
}

type reportedError struct{ status int }

func (e *reportedError) Error() string { return "preflight needs attention" }
func needsStatus(needs []api.Need) int {
	for _, n := range needs {
		if !n.Warning {
			switch n.Code {
			case "approval_required":
				return 2
			case "image_required":
				return 3
			default:
				return 4
			}
		}
	}
	return 0
}

func (o *options) preflight(cmd *cobra.Command, r *unibox.Request) error {
	for attempt := 0; attempt < 4; attempt++ {
		check, err := o.manager.Check(cmd.Context(), *r)
		if err != nil {
			return err
		}
		if check.Ready {
			return nil
		}
		changed := false
		for _, n := range check.Needs {
			if n.Warning {
				continue
			}
			switch n.Code {
			case "approval_required":
				_, _, p, err := o.manager.Definition(*r)
				if err != nil {
					return err
				}
				before, after, err := o.manager.Approval(p.Name)
				if err != nil {
					return err
				}
				if o.interactive && !o.json {
					b, _ := json.MarshalIndent(map[string]any{"previous": before, "requested": after}, "", "  ")
					fmt.Fprintln(o.log, string(b))
				}
				if !o.ask("Approve host access for profile " + p.Name + "?") {
					return unibox.Fail(2, n.Code, "%s", n.Message)
				}
				if err = o.manager.Approve(p.Name); err != nil {
					return err
				}
				changed = true
			case "image_required":
				_, rt, p, err := o.manager.Definition(*r)
				if err != nil {
					return err
				}
				if o.json {
					return unibox.Fail(3, n.Code, "%s", n.Message)
				}
				if err = o.manager.Diff(cmd.Context(), p.Name, rt.Name(), o.log); err != nil {
					return err
				}
				if !o.yes && !o.ask("Build profile "+p.Name+"?") {
					return unibox.Fail(3, n.Code, "%s", n.Message)
				}
				if err = o.manager.Build(cmd.Context(), p.Name, rt.Name(), false); err != nil {
					return err
				}
				changed = true
			case "uncommitted", "local_commits":
				if r.Push && n.Code == "local_commits" {
					continue
				}
				if o.ask(n.Message + "; continue using the forge version?") {
					r.IgnoreLocal = true
					changed = true
				} else {
					return unibox.Fail(4, n.Code, "%s: %s", n.Repository, n.Message)
				}
			case "branch_not_pushed", "branch_missing":
				if !r.Push {
					return unibox.Fail(4, n.Code, "%s: %s", n.Repository, n.Message)
				}
			default:
				return unibox.Fail(4, n.Code, "%s", n.Message)
			}
		}
		if !changed {
			return nil
		}
	}
	return fmt.Errorf("preparation inputs keep changing; retry after the profile is stable")
}

func (o *options) taskCommand(name string) *cobra.Command {
	var req unibox.Request
	c := &cobra.Command{Use: name + " [directory...] [-- agent-args...]", Short: map[string]string{"check": "Explain what a task needs before it can start", "prepare": "Prepare independent forge clones inside a sandbox", "run": "Prepare or resume a task and run its agent in this terminal"}[name]}
	taskFlags(c, &req)
	c.RunE = func(cmd *cobra.Command, args []string) error {
		extra := []string{}
		if n := cmd.ArgsLenAtDash(); n >= 0 {
			extra = args[n:]
			args = args[:n]
		}
		if name != "run" && len(extra) > 0 {
			return fmt.Errorf("only run accepts agent arguments after --")
		}
		for _, dir := range args {
			req.Repositories = append(req.Repositories, unibox.RepoInput{Source: dir})
		}
		if len(req.Repositories) == 0 && req.Task == "" {
			req.Repositories = []unibox.RepoInput{{Source: "."}}
		}
		if name == "check" {
			check, err := o.manager.Check(cmd.Context(), req)
			if err != nil {
				return err
			}
			if o.json {
				if err = o.write(check); err != nil {
					return err
				}
			} else {
				if check.Ready {
					fmt.Fprintln(o.output, "ready (URL-only repositories are checked inside the sandbox)")
				}
				for _, n := range check.Needs {
					fmt.Fprintf(o.output, "%s: %s %s\n", n.Code, n.Repository, n.Message)
				}
			}
			if status := needsStatus(check.Needs); status != 0 {
				return &reportedError{status}
			}
			return nil
		}
		if name == "run" && o.json {
			return fmt.Errorf("run owns the terminal; use check or prepare with --json, then run without it")
		}
		if err := o.preflight(cmd, &req); err != nil {
			return err
		}
		if name == "prepare" {
			result, err := o.manager.Prepare(cmd.Context(), req)
			if err != nil {
				return err
			}
			if o.json {
				return o.write(result)
			}
			fmt.Fprintf(o.output, "task %s ready in sandbox %s (%s)\n", result.Task, result.Sandbox, result.Directory)
			return nil
		}
		return o.manager.Run(cmd.Context(), req, o.input, o.output, o.log, extra)
	}
	return c
}

func (o *options) createCommand() *cobra.Command {
	var req unibox.Request
	c := &cobra.Command{Use: "create sandbox", Args: cobra.ExactArgs(1), Short: "Create a named environment without mounting a host workspace", RunE: func(cmd *cobra.Command, args []string) error {
		req.NewSandbox = args[0]
		if err := o.preflight(cmd, &req); err != nil {
			return err
		}
		b, err := o.manager.Create(cmd.Context(), req)
		if err != nil {
			return err
		}
		if o.json {
			return o.write(map[string]any{"api_version": api.Version, "sandbox": b})
		}
		fmt.Fprintf(o.output, "created sandbox %s (%s: %s)\n", b.Name, b.Runtime, b.RuntimeName)
		return nil
	}}
	c.Flags().StringVar(&req.Profile, "profile", "", "Environment profile")
	c.Flags().StringVar(&req.Runtime, "runtime", "", "Runtime adapter")
	c.Flags().StringSliceVar(&req.Instructions, "instructions", nil, "Named instruction texts to attach")
	return c
}

// Package cli exposes unibox through Cobra, without importing unagit's commands.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tobola/unagit/internal/unibox"
	"github.com/tobola/unagit/internal/unibox/api"
	"golang.org/x/term"
)

type options struct {
	paths       unibox.Paths
	json, yes   bool
	input       io.Reader
	output, log io.Writer
	manager     *unibox.Manager
	interactive bool
}

func New(paths unibox.Paths, in io.Reader, out, errout io.Writer) *cobra.Command {
	o := &options{paths: paths, input: in, output: out, log: errout}
	if f, ok := in.(*os.File); ok {
		o.interactive = term.IsTerminal(int(f.Fd()))
	}
	root := &cobra.Command{Use: "unibox", Short: "Run coding agents in independent sandbox environments", SilenceErrors: true, SilenceUsage: true, Version: "0.1.0"}
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errout)
	root.PersistentFlags().BoolVar(&o.json, "json", false, "Write versioned JSON without prompting")
	root.PersistentFlags().BoolVarP(&o.yes, "yes", "y", false, "Accept operation prompts (does not approve host access or discard work)")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		m, err := unibox.Open(paths, errout)
		if err != nil {
			return err
		}
		o.manager = m
		return m.RefreshPlugin()
	}
	root.AddCommand(o.configCommand(), o.initCommand(), o.validateCommand(), o.inspectCommand(), o.profilesCommand(), o.runtimesCommand(), o.approveCommand(), o.buildCommand(), o.diffCommand(), o.instructionsCommand(), o.createCommand(), o.listCommand(), o.tasksCommand(), o.resumeCommand(), o.shellCommand(), o.recreateCommand(), o.stopCommand(), o.removeCommand(false), o.removeCommand(true), o.herdrCommand())
	for _, name := range []string{"check", "prepare", "run"} {
		root.AddCommand(o.taskCommand(name))
	}
	return root
}

func Execute(ctx context.Context, args []string, in io.Reader, out, errout io.Writer) int {
	paths, err := unibox.DefaultPaths()
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	root := New(paths, in, out, errout)
	root.SetArgs(args)
	err = root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	status := 1
	code := "error"
	var failure *unibox.Failure
	if errors.As(err, &failure) {
		status = failure.Status
		code = failure.Code
	}
	var reported *reportedError
	if errors.As(err, &reported) {
		return reported.status
	}
	jsonMode, _ := root.PersistentFlags().GetBool("json")
	if jsonMode {
		_ = json.NewEncoder(out).Encode(api.Error{APIVersion: api.Version, Code: code, Message: err.Error()})
	} else {
		fmt.Fprintln(errout, "unibox:", err)
	}
	return status
}

func (o *options) write(v any) error {
	e := json.NewEncoder(o.output)
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func (o *options) success(message string) error {
	if o.json {
		return o.write(map[string]any{"api_version": api.Version, "ok": true, "message": message})
	}
	fmt.Fprintln(o.output, message)
	return nil
}
func (o *options) ask(question string) bool {
	if o.json || !o.interactive {
		return false
	}
	fmt.Fprintf(o.log, "%s [y/N] ", question)
	line, _ := bufio.NewReader(o.input).ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes")
}
func (o *options) confirm(question string) error {
	if o.yes || o.ask(question) {
		return nil
	}
	return unibox.Fail(4, "confirmation_required", "%s; rerun with --yes to confirm", strings.ToLower(question))
}

func (o *options) configCommand() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Read or change unibox's own configuration"}
	c.AddCommand(&cobra.Command{Use: "get [key]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg := o.manager.Config
		var value any = cfg
		if len(args) == 1 {
			v, err := configValue(cfg, args[0])
			if err != nil {
				return err
			}
			value = v
		}
		if o.json {
			return o.write(map[string]any{"api_version": api.Version, "value": value})
		}
		switch v := value.(type) {
		case string:
			fmt.Fprintln(o.output, v)
		default:
			b, _ := json.MarshalIndent(v, "", "  ")
			fmt.Fprintln(o.output, string(b))
		}
		return nil
	}})
	c.AddCommand(&cobra.Command{Use: "set key value", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		cfg := o.manager.Config
		key, value := args[0], args[1]
		switch key {
		case "environments":
			path, err := unibox.ExpandPath(value)
			if err != nil {
				return err
			}
			cfg.Environments = path
		case "default_runtime":
			if _, ok := o.manager.Runtimes[value]; !ok {
				return fmt.Errorf("unknown runtime %s; use unibox runtimes", value)
			}
			cfg.DefaultRuntime = value
		default:
			parts := strings.Split(key, ".")
			if len(parts) != 3 || parts[0] != "runtimes" {
				return fmt.Errorf("unknown configuration key %q", key)
			}
			if _, ok := o.manager.Runtimes[parts[1]]; !ok {
				return fmt.Errorf("unknown runtime %s", parts[1])
			}
			v, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("%s must be true or false", key)
			}
			r := cfg.Runtimes[parts[1]]
			switch parts[2] {
			case "on":
				r.On = &v
			case "stop_idle":
				r.StopIdle = &v
			default:
				return fmt.Errorf("unknown configuration key %q", key)
			}
			cfg.Runtimes[parts[1]] = r
		}
		if err := o.paths.SaveConfig(cfg); err != nil {
			return err
		}
		return o.success("configuration updated")
	}})
	return c
}
func configValue(c unibox.Config, key string) (any, error) {
	switch key {
	case "environments":
		return c.Environments, nil
	case "default_runtime":
		return c.DefaultRuntime, nil
	case "runtimes":
		return c.Runtimes, nil
	}
	p := strings.Split(key, ".")
	if len(p) == 3 && p[0] == "runtimes" {
		switch p[2] {
		case "on":
			return c.Enabled(p[1]), nil
		case "stop_idle":
			return c.StopIdle(p[1]), nil
		}
	}
	return nil, fmt.Errorf("unknown configuration key %q", key)
}

func (o *options) profilesCommand() *cobra.Command {
	return &cobra.Command{Use: "profiles", Args: cobra.NoArgs, Short: "List profiles, agents, builds and approval", RunE: func(cmd *cobra.Command, args []string) error {
		rows, err := o.manager.Profiles(cmd.Context())
		if err != nil {
			return err
		}
		if o.json {
			return o.write(map[string]any{"api_version": api.Version, "profiles": rows})
		}
		w := tabwriter.NewWriter(o.output, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "PROFILE\tAGENTS\tIMAGE\tAPPROVED\tERROR")
		for _, p := range rows {
			fmt.Fprintf(w, "%s\t%s\t%v\t%t\t%s\n", p.Name, strings.Join(p.Agents, ","), p.Images, p.Approved, p.Error)
		}
		return w.Flush()
	}}
}
func (o *options) runtimesCommand() *cobra.Command {
	return &cobra.Command{Use: "runtimes", Args: cobra.NoArgs, Short: "Show installed adapters and their isolation capabilities", RunE: func(cmd *cobra.Command, args []string) error {
		rows := []map[string]any{}
		for name, r := range o.manager.Runtimes {
			reason := ""
			if err := r.Available(cmd.Context()); err != nil {
				reason = err.Error()
			}
			rows = append(rows, map[string]any{"name": name, "enabled": o.manager.Config.Enabled(name), "available": reason == "", "error": reason, "capabilities": r.Capabilities()})
		}
		return o.write(map[string]any{"api_version": api.Version, "runtimes": rows})
	}}
}
func (o *options) approveCommand() *cobra.Command {
	return &cobra.Command{Use: "approve profile", Args: cobra.ExactArgs(1), Short: "Explicitly approve a profile's current host access", RunE: func(cmd *cobra.Command, args []string) error {
		before, after, err := o.manager.Approval(args[0])
		if err != nil {
			return err
		}
		if !o.json {
			b, _ := json.MarshalIndent(map[string]any{"previous": before, "approved": after}, "", "  ")
			fmt.Fprintln(o.output, string(b))
		}
		if err = o.manager.Approve(args[0]); err != nil {
			return err
		}
		return o.success("profile approved")
	}}
}
func (o *options) buildCommand() *cobra.Command {
	var rt string
	var rebuild bool
	c := &cobra.Command{Use: "build profile", Args: cobra.ExactArgs(1), Short: "Build, validate and load a profile image", RunE: func(cmd *cobra.Command, args []string) error {
		if !o.yes {
			if err := o.manager.Diff(cmd.Context(), args[0], rt, o.log); err != nil {
				return err
			}
			if err := o.confirm("Build profile " + args[0] + "?"); err != nil {
				return err
			}
		}
		if err := o.manager.Build(cmd.Context(), args[0], rt, rebuild); err != nil {
			return err
		}
		return o.success("image ready")
	}}
	c.Flags().StringVar(&rt, "runtime", "", "Runtime to build for")
	c.Flags().BoolVar(&rebuild, "rebuild", false, "Build again even when current")
	return c
}
func (o *options) diffCommand() *cobra.Command {
	var rt string
	c := &cobra.Command{Use: "diff profile", Args: cobra.ExactArgs(1), Short: "Show changes since the last build", RunE: func(cmd *cobra.Command, args []string) error {
		return o.manager.Diff(cmd.Context(), args[0], rt, o.output)
	}}
	c.Flags().StringVar(&rt, "runtime", "", "Runtime of the previous build")
	return c
}

func (o *options) instructionsCommand() *cobra.Command {
	var sandbox string
	c := &cobra.Command{Use: "instructions [profile]", Args: cobra.MaximumNArgs(1), Short: "Read assembled instructions or attach named texts to a sandbox", RunE: func(cmd *cobra.Command, args []string) error {
		profile := ""
		attached := []string{}
		if sandbox != "" {
			boxes, err := o.manager.List(cmd.Context())
			if err != nil {
				return err
			}
			for _, b := range boxes {
				if b.Name == sandbox {
					profile = b.Profile
					attached = b.Instructions
				}
			}
			if profile == "" {
				return fmt.Errorf("sandbox %s not found", sandbox)
			}
		} else if len(args) == 1 {
			profile = args[0]
		} else {
			return fmt.Errorf("pass a profile or --sandbox")
		}
		p, err := unibox.LoadProfile(o.manager.Config.Environments, profile)
		if err != nil {
			return err
		}
		text, err := p.InstructionText(attached)
		if err != nil {
			return err
		}
		if o.json {
			return o.write(map[string]any{"api_version": api.Version, "instructions": text})
		}
		_, err = io.WriteString(o.output, text)
		return err
	}}
	c.Flags().StringVar(&sandbox, "sandbox", "", "Show a sandbox's assembled instructions")
	c.AddCommand(&cobra.Command{Use: "attach sandbox [instruction...]", Args: cobra.MinimumNArgs(1), Short: "Replace the sandbox's list of attached instruction names", RunE: func(cmd *cobra.Command, args []string) error {
		if err := o.manager.AttachInstructions(args[0], args[1:]); err != nil {
			return err
		}
		return o.success("instructions updated; they apply before the next session")
	}})
	return c
}

func (o *options) listCommand() *cobra.Command {
	return &cobra.Command{Use: "ls", Args: cobra.NoArgs, Short: "List unibox's sandboxes and their runtime state", RunE: func(cmd *cobra.Command, args []string) error {
		boxes, err := o.manager.List(cmd.Context())
		if err != nil {
			return err
		}
		if o.json {
			return o.write(map[string]any{"api_version": api.Version, "sandboxes": boxes})
		}
		w := tabwriter.NewWriter(o.output, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "SANDBOX\tRUNTIME\tPROFILE\tSTATE\tTASKS\tSESSIONS")
		for _, b := range boxes {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\n", b.Name, b.Runtime, b.Profile, b.State, len(b.Tasks), len(b.Runs))
		}
		return w.Flush()
	}}
}
func (o *options) tasksCommand() *cobra.Command {
	return &cobra.Command{Use: "tasks [sandbox]", Args: cobra.MaximumNArgs(1), Short: "List prepared tasks, in every sandbox or in one", RunE: func(cmd *cobra.Command, args []string) error {
		boxes, err := o.manager.List(cmd.Context())
		if err != nil {
			return err
		}
		tasks, runs := []api.Task{}, []api.Run{}
		found := false
		for _, b := range boxes {
			if len(args) == 1 && b.Name != args[0] {
				continue
			}
			found = true
			sorted := slices.Clone(b.Tasks)
			slices.SortFunc(sorted, func(x, y api.Task) int { return strings.Compare(x.ID, y.ID) })
			tasks = append(tasks, sorted...)
			runs = append(runs, b.Runs...)
		}
		if len(args) == 1 && !found {
			return fmt.Errorf("sandbox %s not found; unibox ls lists them", args[0])
		}
		if o.json {
			return o.write(map[string]any{"api_version": api.Version, "tasks": tasks, "runs": runs})
		}
		w := tabwriter.NewWriter(o.output, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "TASK\tSANDBOX\tAGENT\tSTATE\tSESSIONS\tREPOSITORIES")
		for _, t := range tasks {
			sessions := 0
			for _, r := range runs {
				if r.Sandbox == t.Sandbox && r.Task == t.ID {
					sessions++
				}
			}
			repos := []string{}
			for _, r := range t.Repositories {
				repos = append(repos, r.Name+"@"+r.Branch)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", t.ID, t.Sandbox, t.Agent, t.State, sessions, strings.Join(repos, ", "))
		}
		if err = w.Flush(); err != nil {
			return err
		}
		if len(tasks) > 0 {
			fmt.Fprintln(o.log, "resume one with: unibox resume <task>")
		}
		return nil
	}}
}

func (o *options) resumeCommand() *cobra.Command {
	var req unibox.Request
	c := &cobra.Command{Use: "resume task [-- agent-args...]", Args: cobra.MinimumNArgs(1), Short: "Run a prepared task's agent again, in the sandbox that holds it", RunE: func(cmd *cobra.Command, args []string) error {
		if n := cmd.ArgsLenAtDash(); n > 1 || (n < 0 && len(args) > 1) {
			return fmt.Errorf("resume takes one task; agent arguments go after --")
		}
		if o.json {
			return fmt.Errorf("resume owns the terminal; omit --json")
		}
		req.Task = args[0]
		if err := o.preflight(cmd, &req); err != nil {
			return err
		}
		return o.manager.Run(cmd.Context(), req, o.input, o.output, o.log, args[1:])
	}}
	c.Flags().StringVar(&req.Agent, "agent", "", "Coding agent to run instead of the task's own")
	return c
}

func (o *options) shellCommand() *cobra.Command {
	return &cobra.Command{Use: "shell sandbox", Args: cobra.ExactArgs(1), Short: "Open an interactive shell (counts as an active session)", RunE: func(cmd *cobra.Command, args []string) error {
		if o.json {
			return fmt.Errorf("shell is interactive; omit --json")
		}
		return o.manager.Shell(cmd.Context(), args[0], o.input, o.output, o.log)
	}}
}
func (o *options) stopCommand() *cobra.Command {
	return &cobra.Command{Use: "stop sandbox", Args: cobra.ExactArgs(1), Short: "Stop an idle sandbox without deleting its disk", RunE: func(cmd *cobra.Command, args []string) error {
		if err := o.manager.Stop(cmd.Context(), args[0]); err != nil {
			return err
		}
		return o.success("sandbox stopped")
	}}
}
func (o *options) recreateCommand() *cobra.Command {
	var rebuild bool
	c := &cobra.Command{Use: "recreate sandbox", Args: cobra.ExactArgs(1), Short: "Rebuild when needed and recreate an idle sandbox without discarding unpushed work", RunE: func(cmd *cobra.Command, args []string) error {
		if err := o.confirm("Recreate sandbox " + args[0] + " and discard its caches?"); err != nil {
			return err
		}
		// A login stored after the sandbox is made does not reach it (a
		// token's placeholder is set at creation), so ask for it first.
		if err := o.preflight(cmd, &unibox.Request{Sandbox: args[0]}); err != nil {
			return err
		}
		if err := o.manager.Recreate(cmd.Context(), args[0], rebuild); err != nil {
			return err
		}
		return o.success("sandbox recreated")
	}}
	c.Flags().BoolVar(&rebuild, "rebuild", false, "Force a fresh image build")
	return c
}
func (o *options) removeCommand(task bool) *cobra.Command {
	use := "rm sandbox"
	n := 1
	if task {
		use = "rm-task sandbox task"
		n = 2
	}
	return &cobra.Command{Use: use, Args: cobra.ExactArgs(n), Short: "Remove only after checking for running sessions and unpushed work", RunE: func(cmd *cobra.Command, args []string) error {
		if err := o.confirm("Remove " + strings.Join(args, "/") + "?"); err != nil {
			return err
		}
		id := ""
		if task {
			id = args[1]
		}
		if err := o.manager.Remove(cmd.Context(), args[0], id); err != nil {
			return err
		}
		return o.success("removed")
	}}
}

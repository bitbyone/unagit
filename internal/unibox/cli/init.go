package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tobola/unagit/internal/unibox"
	"github.com/tobola/unagit/internal/unibox/api"
)

func (o *options) prompt(reader *bufio.Reader, question, preset string) (string, error) {
	fmt.Fprintf(o.log, "%s [%s]: ", question, preset)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("interactive init was cut short; rerun it or pass the profile on the command line")
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return preset, nil
	}
	return line, nil
}

func (o *options) validateCommand() *cobra.Command {
	return &cobra.Command{Use: "validate", Args: cobra.NoArgs, Short: "Check the environments repository: layout, profiles, instruction texts", RunE: func(cmd *cobra.Command, args []string) error {
		problems, err := unibox.Validate(o.manager.Config.Environments)
		if err != nil {
			return err
		}
		if o.json {
			if err := o.write(map[string]any{"api_version": api.Version, "ok": len(problems) == 0, "problems": problems}); err != nil {
				return err
			}
		} else {
			for _, p := range problems {
				fmt.Fprintf(o.output, "%s: %s\n", p.Subject, p.Message)
			}
			if len(problems) == 0 {
				fmt.Fprintln(o.output, "environments repository is valid")
			}
		}
		if len(problems) > 0 {
			return &reportedError{4}
		}
		return nil
	}}
}

func (o *options) initCommand() *cobra.Command {
	var interactive bool
	var agents []string
	var cpus int
	var memory string
	c := &cobra.Command{Use: "init [profile]", Args: cobra.MaximumNArgs(1), Short: "Lay out the environments repository and add a profile", RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) == 1 {
			name = args[0]
		}
		if interactive {
			if o.json || !o.interactive {
				return fmt.Errorf("interactive init needs a terminal; pass the profile and flags instead")
			}
			reader := bufio.NewReader(o.input)
			if name == "" {
				v, err := o.prompt(reader, "Profile name", "dev")
				if err != nil {
					return err
				}
				name = v
			}
			ids := []string{}
			for _, a := range unibox.Agents {
				ids = append(ids, a.ID)
			}
			v, err := o.prompt(reader, "Agents ("+strings.Join(ids, ", ")+")", strings.Join(agents, ","))
			if err != nil {
				return err
			}
			agents = strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })
			v, err = o.prompt(reader, "CPUs", strconv.Itoa(cpus))
			if err != nil {
				return err
			}
			if cpus, err = strconv.Atoi(v); err != nil {
				return fmt.Errorf("cpus must be a number")
			}
			if memory, err = o.prompt(reader, "Memory", memory); err != nil {
				return err
			}
		}
		if name == "" {
			return fmt.Errorf("name the profile: unibox init <profile>, or ask the questions with -i")
		}
		root := o.manager.Config.Environments
		recorded := false
		if root == "" {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			root = wd
			cfg := o.manager.Config
			cfg.Environments = root
			if err := o.paths.SaveConfig(cfg); err != nil {
				return err
			}
			recorded = true
		}
		report, err := unibox.Init(root, name, agents, cpus, memory)
		if err != nil {
			return err
		}
		if o.json {
			return o.write(map[string]any{"api_version": api.Version, "root": report.Root, "written": report.Written, "kept": report.Kept})
		}
		if recorded {
			fmt.Fprintf(o.output, "environments repository recorded: %s\n", root)
		}
		for _, rel := range report.Written {
			fmt.Fprintln(o.output, "wrote", rel)
		}
		for _, rel := range report.Kept {
			fmt.Fprintln(o.output, "kept", rel)
		}
		fmt.Fprintf(o.output, "profile %s is ready to approve and build: unibox approve %s && unibox build %s\n", name, name, name)
		return nil
	}}
	c.Flags().BoolVarP(&interactive, "interactive", "i", false, "Ask for the profile's name, agents and resources")
	c.Flags().StringSliceVar(&agents, "agents", []string{"copilot"}, "Agents the profile declares and installs")
	c.Flags().IntVar(&cpus, "cpus", 4, "CPUs for the profile's sandboxes")
	c.Flags().StringVar(&memory, "memory", "8g", "Memory for the profile's sandboxes")
	return c
}

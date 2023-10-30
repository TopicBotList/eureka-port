package cmd

import (
	"fmt"
	"os"
	"runtime/debug"

	"golang.org/x/exp/slices"
)

// A struct that holds the state of the command line
type CommandLineState struct {
	// The map of commands
	Commands map[string]Command

	// The function that returns the header (program name, version, etc.)
	GetHeader func() string
}

// Helper method that returns the git commit hash
func GetGitCommit() string {
	var gitCommit string

	// Use runtime/debug vcs.revision to get the git commit hash
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				gitCommit = setting.Value
			}
		}
	}

	return gitCommit
}

type Command struct {
	Func        func(progname string, args []string)
	Help        string
	Usage       string
	Example     string
	Subcommands map[string]Command
	ArgValidate func(args []string) error
}

func (c *Command) Validate(args []string) error {
	if c.ArgValidate != nil {
		err := c.ArgValidate(args)

		if err != nil {
			return fmt.Errorf("invalid arguments: %w", err)
		}
	}

	return nil
}

func FindCommandByArgs(cmds map[string]Command, args []string) (*Command, []string, error) {
	if len(args) == 0 {
		return nil, args, fmt.Errorf("no command provided")
	}

	c, ok := cmds[args[0]]
	if !ok {
		return nil, args, fmt.Errorf("unknown command: %s", args[0])
	}

	if c.Subcommands != nil {
		if len(args) < 2 {
			if c.Func != nil {
				return &c, args, nil
			}

			return &c, args, fmt.Errorf("no subcommand provided")
		}

		subcmd, ok := c.Subcommands[args[1]]

		if !ok {
			return &c, args, fmt.Errorf("unknown subcommand: %s", args[0]+" "+args[1])
		}

		c = subcmd
		args = args[2:]

		if c.Subcommands != nil {
			if len(args) > 0 {
				return FindCommandByArgs(c.Subcommands, args)
			} else if c.Func == nil {
				return &c, args, fmt.Errorf("no subcommand provided")
			}
		}
	} else {
		args = args[1:]
	}

	return &c, args, nil
}

func (c *Command) GetUsage() string {
	initial := c.Help

	if c.Usage != "" {
		initial += "\n\nUsage: " + c.Usage
	}

	if c.Example != "" {
		initial += "\n\nExample: " + c.Example
	}

	if c.Subcommands != nil {
		initial += "\n\nSubcommands:"

		for k, cmd := range c.Subcommands {
			initial += fmt.Sprintf("\n%s: %s", k, cmd.Help)
		}
	}

	return initial
}

func CmdListToArray(cmds map[string]Command) []string {
	s := []string{"Commands:"}
	for k, cmd := range cmds {
		s = append(s, fmt.Sprint(k+": ", cmd.Help))
	}

	return s
}

func CmdList(cmds map[string]Command) {
	for _, cmd := range CmdListToArray(cmds) {
		fmt.Println(cmd)
	}
}

func (s *CommandLineState) Run() {
	progname := os.Args[0]
	args := os.Args[1:]

	if len(args) == 0 {
		fmt.Printf("usage: %s <command> [args]\n\n", progname)
		CmdList(s.Commands)
		os.Exit(1)
	}

	cmd, args, err := FindCommandByArgs(s.Commands, args)

	if slices.Contains(args, "-h") || slices.Contains(args, "--help") {
		fmt.Printf("%s\n\n", s.GetHeader())
		fmt.Printf("structure: %s <command> [args]\n\n", progname)

		if cmd != nil {
			fmt.Printf("%s\n\n", cmd.GetUsage())
		} else {
			CmdList(s.Commands)
		}

		os.Exit(1)
	}

	if err != nil {
		fmt.Printf("error: %s\n\n", err)

		if cmd != nil {
			fmt.Printf("structure: %s [args]\n%s\n\n", progname, cmd.GetUsage())
		} else {
			CmdList(s.Commands)
		}

		os.Exit(1)
	}

	if err := cmd.Validate(args); err != nil {
		fmt.Printf("error: %s\n\n", err)
		fmt.Printf("structure: %s [args]\n%s\n\n", progname, cmd.GetUsage())
		os.Exit(1)
	}

	cmd.Func(progname, args)
}

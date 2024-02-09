package shellcli

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/go-andiamo/splitter"
)

// ShellCli is a simple shell-like interface with commands
type ShellCli[T any] struct {
	Commands        map[string]*Command[T]
	Splitter        splitter.Splitter
	ArgSplitter     splitter.Splitter
	CaseInsensitive bool
	Prompter        func(*ShellCli[T]) string
	Data            *T
}

// Returns a help command
func (s *ShellCli[T]) Help() *Command[T] {
	return &Command[T]{
		Description: "Get help for a command",
		Args: [][3]string{
			{"command", "Command to get help for", ""},
		},
		Run: func(a *ShellCli[T], args map[string]string) error {
			if arg, ok := args["command"]; ok && arg != "" {
				cmd, ok := a.Commands[arg]

				if !ok {
					return fmt.Errorf("unknown command: %s", arg)
				}

				fmt.Println("Command: ", arg)
				fmt.Println("Description: ", cmd.Description)
				fmt.Println("Arguments: ")

				for _, cmd := range cmd.Args {
					fmt.Print("  ", cmd[0], " : ", cmd[1], " (default: ", cmd[2], ")\n")
				}
			} else {
				fmt.Println("Commands: ")

				for cmd, desc := range a.Commands {
					fmt.Print("  ", cmd, ": ", desc.Description, "\n")
				}

				fmt.Println("Use 'help <command>' to get help for a specific command")
			}

			return nil
		},
	}
}

// Command is a command for the shell client
type Command[T any] struct {
	Description string
	Args        [][3]string // Map of argument to the description and default value
	Run         func(a *ShellCli[T], args map[string]string) error
}

// Init initializes the shell client
func (a *ShellCli[T]) Init() error {
	var err error
	a.Splitter, err = splitter.NewSplitter(' ', splitter.DoubleQuotes, splitter.SingleQuotes)

	if err != nil {
		return fmt.Errorf("error initializing tokenizer: %s", err)
	}

	a.Splitter.AddDefaultOptions(splitter.IgnoreEmptyFirst, splitter.IgnoreEmptyLast, splitter.TrimSpaces, splitter.UnescapeQuotes)

	a.ArgSplitter, err = splitter.NewSplitter('=', splitter.DoubleQuotes, splitter.SingleQuotes)

	if err != nil {
		return fmt.Errorf("error initializing arg tokenizer: %s", err)
	}

	a.ArgSplitter.AddDefaultOptions(splitter.IgnoreEmptyFirst, splitter.IgnoreEmptyLast, splitter.TrimSpaces, splitter.UnescapeQuotes)

	return nil
}

// Exec executes a command
func (a *ShellCli[T]) Exec(cmd []string) error {
	if len(cmd) == 0 {
		return nil
	}

	cmdName := cmd[0]

	if a.CaseInsensitive {
		cmdName = strings.ToLower(cmdName)
	}

	cmdData, ok := a.Commands[cmdName]

	if !ok {
		return fmt.Errorf("unknown command: %s", cmd[0])
	}

	args := cmd[1:]

	argMap := make(map[string]string)

	for i, arg := range args {
		fields, err := a.ArgSplitter.Split(arg)

		if err != nil {
			return fmt.Errorf("error splitting argument: %s", err)
		}

		if len(fields) == 1 {
			if len(cmdData.Args) <= i {
				fmt.Println("WARNING: extra argument: ", fields[0])
			}

			argMap[cmdData.Args[i][0]] = fields[0]

			continue
		}

		if len(fields) != 2 {
			return fmt.Errorf("invalid argument: %s", arg)
		}

		argMap[fields[0]] = fields[1]
	}

	err := cmdData.Run(a, argMap)

	if err != nil {
		return err
	}

	return nil
}

func (a *ShellCli[T]) Prompt() error {
	fmt.Print(a.Prompter(a))

	buf := bufio.NewReader(os.Stdin)
	var command, err = buf.ReadString('\n')

	if err != nil {
		return err
	}

	command = strings.TrimSpace(command)

	tokens, err := a.Splitter.Split(command)

	if err != nil {
		return fmt.Errorf("error splitting command: %s", err)
	}

	if len(tokens) == 0 || tokens[0] == "" {
		return nil
	}

	err = a.Exec(tokens)

	if err != nil {
		return err
	}

	return nil
}

// AddCommand adds a command to the shell client
//
// It is recommended to use this to add a command over directly modifying the Commands map
// as this function will be updated to be backwards compatible with future changes
func (a *ShellCli[T]) AddCommand(name string, cmd *Command[T]) {
	if a.Commands == nil {
		a.Commands = make(map[string]*Command[T])
	}

	a.Commands[name] = cmd
}

// Run constantly prompts for input and os.Exit()'s on interrupt signal
//
// Only use this for actual shell apps
func (a *ShellCli[T]) Run() {
	err := a.Init()

	if err != nil {
		fmt.Println("Error initializing animuscli: ", err)
		os.Exit(1)
	}

	go func() {
		for {
			err = a.Prompt()

			if err != nil {
				fmt.Println("Error: ", err)
			}
		}
	}()

	// Wait for signals
	signals := []os.Signal{os.Interrupt, os.Kill}

	var channel = make(chan os.Signal, 1)
	signal.Notify(channel, signals...)

	<-channel

	fmt.Println("\nExiting...")
}

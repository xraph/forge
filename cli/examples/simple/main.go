package main

import (
	"fmt"
	"os"

	"github.com/xraph/forge/cli"
)

func main() {
	// Create a simple CLI application
	app := cli.New(cli.Config{
		Name:        "simple",
		Version:     "1.0.0",
		Description: "A simple CLI example",
	})

	// Add a hello command
	helloCmd := cli.NewCommand(
		"hello",
		"Say hello",
		func(ctx cli.CommandContext) error {
			name := ctx.String("name")
			if name == "" {
				name = "World"
			}

			ctx.Success(fmt.Sprintf("Hello, %s!", name))
			return nil
		},
		cli.WithFlag(cli.NewStringFlag("name", "n", "Name to greet", "")),
	)

	app.AddCommand(helloCmd)

	// Add a goodbye command
	goodbyeCmd := cli.NewCommand(
		"goodbye",
		"Say goodbye",
		func(ctx cli.CommandContext) error {
			name := ctx.String("name")
			if name == "" {
				name = "World"
			}

			ctx.Info(fmt.Sprintf("Goodbye, %s!", name))
			return nil
		},
		cli.WithFlag(cli.NewStringFlag("name", "n", "Name to say goodbye to", "")),
	)

	app.AddCommand(goodbyeCmd)

	// Run the CLI
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cli.GetExitCode(err))
	}
}

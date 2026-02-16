package main

import (
	"context"
	"fmt"
	"os"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"
)

// ExampleService demonstrates a service that can be accessed from CLI
type ExampleService struct {
	name string
}

func NewExampleService() *ExampleService {
	return &ExampleService{
		name: "example-service",
	}
}

func (s *ExampleService) Process(data string) string {
	return fmt.Sprintf("Processed: %s", data)
}

func main() {
	// Create Forge app
	app := forge.NewApp(forge.AppConfig{
		Name:        "myapp",
		Version:     "1.0.0",
		Description: "Application with Forge integration",
		Environment: "development",
	})

	// Register a service
	_ = forge.Provide(app.Container(), func() (*ExampleService, error) {
		return NewExampleService(), nil
	})

	// Create CLI with Forge integration
	cliApp := cli.NewForgeIntegratedCLI(app, cli.Config{
		Name:        "myapp-cli",
		Version:     "1.0.0",
		Description: "CLI with Forge integration",
	})

	// Add a command that uses Forge services
	processCmd := cli.NewCommand(
		"process",
		"Process data using Forge service",
		func(ctx cli.CommandContext) error {
			data := ctx.String("data")
			if data == "" {
				return cli.NewError("data is required", cli.ExitUsageError)
			}

			// Access Forge service via DI
			service, err := cli.GetService[*ExampleService](ctx)
			if err != nil {
				return cli.WrapError(err, "failed to get service", cli.ExitError)
			}

			result := service.Process(data)
			ctx.Success(result)

			return nil
		},
		cli.WithFlag(cli.NewStringFlag("data", "d", "Data to process", "", cli.Required())),
	)

	cliApp.AddCommand(processCmd)

	// Add a command that shows DI container info
	servicesCmd := cli.NewCommand(
		"services",
		"List registered services",
		func(ctx cli.CommandContext) error {
			forgeApp := cli.MustGetApp(ctx)
			services := forgeApp.Container().Services()

			table := ctx.Table()
			table.SetHeader([]string{"Service", "Status"})

			for _, name := range services {
				status := cli.Green("✓ Available")
				table.AppendRow([]string{name, status})
			}

			table.Render()

			return nil
		},
	)

	cliApp.AddCommand(servicesCmd)

	// Start Forge app in background if needed
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Note: For CLI-only mode, we don't start the HTTP server
	// Just ensure DI container is ready
	_ = ctx

	// Run the CLI
	if err := cliApp.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cli.GetExitCode(err))
	}
}

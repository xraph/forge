package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/xraph/forge/cli"
)

func main() {
	app := cli.New(cli.Config{
		Name:        "async-demo",
		Version:     "1.0.0",
		Description: "Demo of async select and spinner",
	})

	// Example 1: Basic Spinner
	spinnerCmd := cli.NewCommand(
		"spinner",
		"Show spinner animation",
		func(ctx cli.CommandContext) error {
			spinner := ctx.Spinner("Processing data...")

			// Simulate work
			time.Sleep(3 * time.Second)

			spinner.Stop(cli.Green("✓ Complete!"))
			return nil
		},
	)

	// Example 2: Spinner with updates
	spinnerUpdateCmd := cli.NewCommand(
		"spinner-update",
		"Show spinner with status updates",
		func(ctx cli.CommandContext) error {
			spinner := ctx.Spinner("Starting...")

			// Simulate multi-step process
			time.Sleep(1 * time.Second)
			spinner.Update("Loading configuration...")
			time.Sleep(1 * time.Second)
			spinner.Update("Connecting to database...")
			time.Sleep(1 * time.Second)
			spinner.Update("Fetching data...")
			time.Sleep(1 * time.Second)

			spinner.Stop(cli.Green("✓ All done!"))
			return nil
		},
	)

	// Example 3: Async Select
	asyncSelectCmd := cli.NewCommand(
		"async-select",
		"Select from async-loaded options",
		func(ctx cli.CommandContext) error {
			// Define async loader
			loader := func(ctx context.Context) ([]string, error) {
				// Simulate API call or database query
				time.Sleep(2 * time.Second)
				return []string{
					"Production",
					"Staging",
					"Development",
					"Testing",
				}, nil
			}

			// Use async select
			env, err := ctx.SelectAsync("Choose environment:", loader)
			if err != nil {
				return err
			}

			ctx.Success(fmt.Sprintf("Selected: %s", env))
			return nil
		},
	)

	// Example 4: Async Multi-Select
	asyncMultiSelectCmd := cli.NewCommand(
		"async-multi-select",
		"Multi-select from async-loaded options",
		func(ctx cli.CommandContext) error {
			// Define async loader (e.g., fetch from API)
			loader := func(ctx context.Context) ([]string, error) {
				// Simulate fetching available features from server
				time.Sleep(2 * time.Second)
				return []string{
					"Authentication",
					"Database",
					"Cache",
					"Events",
					"Monitoring",
					"Logging",
				}, nil
			}

			// Use async multi-select
			features, err := ctx.MultiSelectAsync("Select features to enable:", loader)
			if err != nil {
				return err
			}

			if len(features) > 0 {
				ctx.Success(fmt.Sprintf("Selected %d features:", len(features)))
				for _, f := range features {
					ctx.Printf("  • %s\n", f)
				}
			} else {
				ctx.Info("No features selected")
			}
			return nil
		},
	)

	// Example 5: Select with Retry (simulating flaky network)
	retrySelectCmd := cli.NewCommand(
		"retry-select",
		"Select with automatic retry on failure",
		func(ctx cli.CommandContext) error {
			attempt := 0
			loader := func(ctx context.Context) ([]string, error) {
				attempt++
				// Simulate flaky API - fail first 2 attempts
				if attempt <= 2 {
					time.Sleep(1 * time.Second)
					return nil, fmt.Errorf("network timeout")
				}

				// Succeed on 3rd attempt
				time.Sleep(1 * time.Second)
				return []string{
					"Region 1 (US-East)",
					"Region 2 (EU-West)",
					"Region 3 (Asia-Pacific)",
				}, nil
			}

			// Try up to 3 times
			region, err := ctx.SelectWithRetry("Choose deployment region:", loader, 3)
			if err != nil {
				return err
			}

			ctx.Success(fmt.Sprintf("Deploying to: %s", region))
			return nil
		},
	)

	// Example 6: Real-world workflow
	deployCmd := cli.NewCommand(
		"deploy",
		"Complete deployment workflow with async selects",
		func(ctx cli.CommandContext) error {
			ctx.Info("Starting deployment wizard...")
			ctx.Println("")

			// Step 1: Load and select environment
			envLoader := func(ctx context.Context) ([]string, error) {
				time.Sleep(1 * time.Second)
				return []string{"Production", "Staging", "Development"}, nil
			}

			env, err := ctx.SelectAsync("Step 1: Choose environment:", envLoader)
			if err != nil {
				return err
			}
			ctx.Printf("Environment: %s\n\n", cli.Bold(env))

			// Step 2: Load and select region
			regionLoader := func(ctx context.Context) ([]string, error) {
				time.Sleep(1 * time.Second)
				// Load regions based on environment
				return []string{
					"us-east-1",
					"us-west-2",
					"eu-west-1",
					"ap-southeast-1",
				}, nil
			}

			region, err := ctx.SelectAsync("Step 2: Choose region:", regionLoader)
			if err != nil {
				return err
			}
			ctx.Printf("Region: %s\n\n", cli.Bold(region))

			// Step 3: Load and multi-select services
			servicesLoader := func(ctx context.Context) ([]string, error) {
				time.Sleep(1 * time.Second)
				// Load available services from the selected environment
				return []string{
					"API Server",
					"Background Workers",
					"Database Migrations",
					"Static Assets",
					"Cache Warmup",
				}, nil
			}

			services, err := ctx.MultiSelectAsync("Step 3: Select services to deploy:", servicesLoader)
			if err != nil {
				return err
			}

			// Step 4: Confirm deployment
			ctx.Println("")
			ctx.Info("Deployment Summary:")
			ctx.Printf("  Environment: %s\n", cli.Bold(env))
			ctx.Printf("  Region: %s\n", cli.Bold(region))
			ctx.Printf("  Services: %d selected\n", len(services))

			confirmed, err := ctx.Confirm("\nProceed with deployment?")
			if err != nil {
				return err
			}

			if !confirmed {
				ctx.Warning("Deployment cancelled")
				return nil
			}

			// Step 5: Deploy with spinner
			ctx.Println("")
			spinner := ctx.Spinner("Deploying services...")

			for i, service := range services {
				time.Sleep(500 * time.Millisecond)
				spinner.Update(fmt.Sprintf("Deploying %s (%d/%d)...", service, i+1, len(services)))
			}

			spinner.Stop(cli.Green("✓ Deployment complete!"))
			ctx.Println("")
			ctx.Success("All services deployed successfully!")

			return nil
		},
	)

	app.AddCommand(spinnerCmd)
	app.AddCommand(spinnerUpdateCmd)
	app.AddCommand(asyncSelectCmd)
	app.AddCommand(asyncMultiSelectCmd)
	app.AddCommand(retrySelectCmd)
	app.AddCommand(deployCmd)

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cli.GetExitCode(err))
	}
}

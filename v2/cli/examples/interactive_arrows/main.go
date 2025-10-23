package main

import (
	"fmt"
	"os"

	"github.com/xraph/forge/v2/cli"
)

func main() {
	app := cli.New(cli.Config{
		Name:        "interactive-demo",
		Version:     "1.0.0",
		Description: "Demo of arrow key navigation and space selection",
	})

	// Single select with arrow keys
	selectCmd := cli.NewCommand(
		"select",
		"Select one option using arrow keys",
		func(ctx cli.CommandContext) error {
			ctx.Info("Use ↑/↓ arrow keys to navigate, Enter to select, or Esc/q to cancel")
			ctx.Println()

			options := []string{
				"Development Environment",
				"Staging Environment",
				"Production Environment",
				"Testing Environment",
			}

			selected, err := ctx.Select("Choose your deployment environment:", options)
			if err != nil {
				return err
			}

			ctx.Success(fmt.Sprintf("You selected: %s", selected))
			return nil
		},
	)

	// Multi-select with arrow keys and space
	multiSelectCmd := cli.NewCommand(
		"multi-select",
		"Select multiple options using arrow keys and space",
		func(ctx cli.CommandContext) error {
			ctx.Info("Use ↑/↓ to navigate, Space to select/deselect, Enter to confirm")
			ctx.Println()

			options := []string{
				"Authentication & Authorization",
				"Database Integration",
				"Caching Layer",
				"Message Queue",
				"Event Streaming",
				"API Gateway",
				"Load Balancer",
				"Monitoring & Logging",
			}

			selected, err := ctx.MultiSelect("Select features to enable:", options)
			if err != nil {
				return err
			}

			if len(selected) == 0 {
				ctx.Info("No features selected")
			} else {
				ctx.Success(fmt.Sprintf("Enabled %d features", len(selected)))
			}

			return nil
		},
	)

	// Long list demo
	longListCmd := cli.NewCommand(
		"long-list",
		"Navigate a long list of options",
		func(ctx cli.CommandContext) error {
			options := []string{
				"Option 1 - First Choice",
				"Option 2 - Second Choice",
				"Option 3 - Third Choice",
				"Option 4 - Fourth Choice",
				"Option 5 - Fifth Choice",
				"Option 6 - Sixth Choice",
				"Option 7 - Seventh Choice",
				"Option 8 - Eighth Choice",
				"Option 9 - Ninth Choice",
				"Option 10 - Tenth Choice",
				"Option 11 - Eleventh Choice",
				"Option 12 - Twelfth Choice",
			}

			selected, err := ctx.Select("Navigate this long list:", options)
			if err != nil {
				return err
			}

			ctx.Success(fmt.Sprintf("You selected: %s", selected))
			return nil
		},
	)

	// Full workflow demo
	workflowCmd := cli.NewCommand(
		"workflow",
		"Complete interactive workflow",
		func(ctx cli.CommandContext) error {
			// Step 1: Select environment
			environments := []string{"Development", "Staging", "Production"}
			env, err := ctx.Select("Step 1: Select environment:", environments)
			if err != nil {
				return err
			}

			// Step 2: Multi-select features
			features := []string{
				"Database",
				"Cache",
				"Events",
				"Monitoring",
			}
			selectedFeatures, err := ctx.MultiSelect("Step 2: Select features:", features)
			if err != nil {
				return err
			}

			// Step 3: Confirm
			confirmed, err := ctx.Confirm(fmt.Sprintf("Deploy to %s with %d features?", env, len(selectedFeatures)))
			if err != nil {
				return err
			}

			if !confirmed {
				ctx.Warning("Deployment cancelled")
				return nil
			}

			// Show summary
			ctx.Println()
			ctx.Success("Deployment Configuration:")

			table := ctx.Table()
			table.SetStyle(cli.StyleRounded)
			table.SetHeader([]string{"Setting", "Value"})
			table.AppendRow([]string{"Environment", cli.Bold(env)})
			table.AppendRow([]string{"Features", fmt.Sprintf("%d selected", len(selectedFeatures))})
			table.AppendRow([]string{"Status", cli.Green("✓ Ready")})
			table.Render()

			return nil
		},
	)

	// Vim-style navigation demo
	vimCmd := cli.NewCommand(
		"vim-keys",
		"Use j/k (Vim-style) for navigation",
		func(ctx cli.CommandContext) error {
			ctx.Info("You can also use j (down) and k (up) for navigation!")
			ctx.Println()

			options := []string{
				"Option A",
				"Option B",
				"Option C",
				"Option D",
			}

			selected, err := ctx.Select("Try Vim-style navigation (j/k):", options)
			if err != nil {
				return err
			}

			ctx.Success(fmt.Sprintf("Selected: %s", selected))
			return nil
		},
	)

	app.AddCommand(selectCmd)
	app.AddCommand(multiSelectCmd)
	app.AddCommand(longListCmd)
	app.AddCommand(workflowCmd)
	app.AddCommand(vimCmd)

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cli.GetExitCode(err))
	}
}

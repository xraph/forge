package main

import (
	"fmt"
	"os"

	"github.com/xraph/forge/cli"
)

func main() {
	app := cli.New(cli.Config{
		Name:        "enhanced-demo",
		Version:     "1.0.0",
		Description: "Demo of enhanced CLI features",
	})

	// Enhanced table styles
	tableStylesCmd := cli.NewCommand(
		"table-styles",
		"Demonstrate different table styles",
		func(ctx cli.CommandContext) error {
			styles := []struct {
				name  string
				style cli.TableStyle
			}{
				{"Default", cli.StyleDefault},
				{"Rounded", cli.StyleRounded},
				{"Simple", cli.StyleSimple},
				{"Compact", cli.StyleCompact},
				{"Markdown", cli.StyleMarkdown},
			}

			for _, s := range styles {
				fmt.Println("\n" + cli.Bold(s.name) + " Style:")
				table := ctx.Table()
				table.SetStyle(s.style)
				table.SetHeader([]string{"ID", "Service", "Status", "Uptime"})
				table.AppendRow([]string{"1", "API", cli.Green("✓ Healthy"), "99.9%"})
				table.AppendRow([]string{"2", "Database", cli.Yellow("⚠ Warning"), "95.0%"})
				table.AppendRow([]string{"3", "Cache", cli.Red("✗ Down"), "0.0%"})
				table.Render()
			}

			return nil
		},
	)

	// Column alignment demo
	alignmentCmd := cli.NewCommand(
		"table-alignment",
		"Demonstrate per-column alignment",
		func(ctx cli.CommandContext) error {
			table := ctx.Table()
			table.SetStyle(cli.StyleRounded)
			table.SetHeader([]string{"Product", "Price", "Quantity", "Total"})

			// Set specific column alignments
			table.SetColumnAlignment(0, cli.AlignLeft)   // Product name
			table.SetColumnAlignment(1, cli.AlignRight)  // Price
			table.SetColumnAlignment(2, cli.AlignCenter) // Quantity
			table.SetColumnAlignment(3, cli.AlignRight)  // Total

			table.AppendRow([]string{"Widget", "$19.99", "5", "$99.95"})
			table.AppendRow([]string{"Gadget", "$49.99", "12", "$599.88"})
			table.AppendRow([]string{"Tool", "$9.99", "100", "$999.00"})

			table.Render()

			return nil
		},
	)

	// Enhanced select demo
	selectCmd := cli.NewCommand(
		"select",
		"Demonstrate enhanced select prompt",
		func(ctx cli.CommandContext) error {
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

			ctx.Success(fmt.Sprintf("You will deploy to: %s", selected))
			return nil
		},
	)

	// Enhanced multi-select demo
	multiSelectCmd := cli.NewCommand(
		"multi-select",
		"Demonstrate enhanced multi-select prompt",
		func(ctx cli.CommandContext) error {
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

	// Long text truncation demo
	truncateCmd := cli.NewCommand(
		"table-truncate",
		"Demonstrate text truncation in tables",
		func(ctx cli.CommandContext) error {
			table := ctx.Table()
			table.SetMaxColumnWidth(30) // Set max column width
			table.SetHeader([]string{"ID", "Description", "Status"})

			table.AppendRow([]string{
				"1",
				"This is a very long description that will be truncated to fit within the column width constraint",
				cli.Green("Active"),
			})
			table.AppendRow([]string{
				"2",
				"Short desc",
				cli.Yellow("Pending"),
			})
			table.AppendRow([]string{
				"3",
				"Another extremely long description with lots of text that exceeds the maximum column width",
				cli.Red("Failed"),
			})

			table.Render()

			return nil
		},
	)

	// All features combined
	combinedCmd := cli.NewCommand(
		"combined",
		"Interactive demo with selection and table display",
		func(ctx cli.CommandContext) error {
			// Select a report type
			reportTypes := []string{
				"Daily Summary",
				"Weekly Overview",
				"Monthly Report",
				"Quarterly Analysis",
			}

			reportType, err := ctx.Select("Select report type:", reportTypes)
			if err != nil {
				return err
			}

			// Multi-select metrics to include
			metrics := []string{
				"Revenue",
				"Active Users",
				"Conversion Rate",
				"Bounce Rate",
				"Page Views",
			}

			selectedMetrics, err := ctx.MultiSelect("Select metrics to include:", metrics)
			if err != nil {
				return err
			}

			// Display result in a styled table
			ctx.Println("\n" + cli.Bold("Report Configuration:"))

			table := ctx.Table()
			table.SetStyle(cli.StyleRounded)
			table.SetHeader([]string{"Setting", "Value"})
			table.AppendRow([]string{"Report Type", cli.Bold(reportType)})
			table.AppendRow([]string{"Metrics Count", fmt.Sprintf("%d", len(selectedMetrics))})
			table.AppendRow([]string{"Status", cli.Green("✓ Ready")})
			table.Render()

			if len(selectedMetrics) > 0 {
				ctx.Println("\n" + cli.Bold("Selected Metrics:"))
				for _, metric := range selectedMetrics {
					ctx.Println(cli.Cyan("  ► ") + metric)
				}
			}

			return nil
		},
	)

	app.AddCommand(tableStylesCmd)
	app.AddCommand(alignmentCmd)
	app.AddCommand(selectCmd)
	app.AddCommand(multiSelectCmd)
	app.AddCommand(truncateCmd)
	app.AddCommand(combinedCmd)

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cli.GetExitCode(err))
	}
}

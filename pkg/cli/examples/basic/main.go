package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/xraph/forge/pkg/cli"
	"github.com/xraph/forge/pkg/cli/middleware"
)

// Example service for dependency injection
type GreetingService interface {
	Greet(name string) string
}

type greetingService struct {
	prefix string
}

func (g *greetingService) Greet(name string) string {
	return fmt.Sprintf("%s, %s!", g.prefix, name)
}

func NewGreetingService() GreetingService {
	return &greetingService{prefix: "Hello"}
}

func main() {
	// Create CLI application
	app := cli.NewCLIApp("basic-cli", "A basic CLI application example")

	// Set version
	app.SetConfig(&cli.CLIConfig{
		Name:        "basic-cli",
		Description: "A basic CLI application example",
		Version:     "1.0.0",
	})

	// Register services
	if err := app.RegisterService(NewGreetingService()); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register greeting service: %v\n", err)
		os.Exit(1)
	}

	// Add middleware
	if err := app.UseMiddleware(middleware.NewLoggingMiddleware(app.Logger())); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to add logging middleware: %v\n", err)
		os.Exit(1)
	}

	if err := app.UseMiddleware(middleware.NewRecoveryMiddleware(app.Logger())); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to add recovery middleware: %v\n", err)
		os.Exit(1)
	}

	// Create commands
	greetCmd := createGreetCommand()
	listCmd := createListCommand()
	interactiveCmd := createInteractiveCommand()

	// Add commands
	if err := app.AddCommand(greetCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to add greet command: %v\n", err)
		os.Exit(1)
	}

	if err := app.AddCommand(listCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to add list command: %v\n", err)
		os.Exit(1)
	}

	if err := app.AddCommand(interactiveCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to add interactive command: %v\n", err)
		os.Exit(1)
	}

	// Enable shell completion
	if err := app.EnableCompletion(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to enable completion: %v\n", err)
		os.Exit(1)
	}

	// Execute application
	if err := app.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// createGreetCommand creates the greet command
func createGreetCommand() *cli.Command {
	cmd := cli.NewCommand("greet", "Greet someone")

	cmd.WithLong("Greet someone with a personalized message").
		WithExample("basic-cli greet --name John --uppercase").
		WithFlags(
			cli.StringFlag("name", "n", "Name to greet", true).WithDefault("World"),
			cli.BoolFlag("uppercase", "u", "Convert to uppercase"),
			cli.StringFlag("prefix", "p", "Greeting prefix", false).WithDefault("Hello"),
		).
		WithService((*GreetingService)(nil))

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		// Resolve greeting service
		var greetingSvc GreetingService
		if err := ctx.Resolve(&greetingSvc); err != nil {
			return fmt.Errorf("failed to resolve greeting service: %w", err)
		}

		// Get flag values
		name, _ := ctx.GetStringFlag("name")
		uppercase, _ := ctx.GetBoolFlag("uppercase")
		prefix, _ := ctx.GetStringFlag("prefix")

		// Update service prefix if provided
		if svc, ok := greetingSvc.(*greetingService); ok && prefix != "Hello" {
			svc.prefix = prefix
		}

		// Generate greeting
		greeting := greetingSvc.Greet(name)
		if uppercase {
			greeting = strings.ToUpper(greeting)
		}

		// Output result
		result := map[string]interface{}{
			"greeting":  greeting,
			"name":      name,
			"uppercase": uppercase,
			"prefix":    prefix,
		}

		return ctx.OutputData(result)
	}

	return cmd
}

// createListCommand creates the list command
func createListCommand() *cli.Command {
	cmd := cli.NewCommand("list", "List items with various options")

	cmd.WithLong("List items with filtering, sorting, and formatting options").
		WithExample("basic-cli list --filter active --sort name --format table").
		WithFlags(
			cli.StringSliceFlag("items", "i", "Items to list", false).WithDefault([]string{"apple", "banana", "cherry"}),
			cli.StringFlag("filter", "f", "Filter items", false),
			cli.StringFlag("sort", "s", "Sort by field", false).WithDefault("name"),
			cli.StringFlag("format", "", "Output format", false).WithDefault("json"),
			cli.IntFlag("limit", "l", "Limit number of results", false).WithDefault(0),
		)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		// Get flag values
		items := ctx.GetStringSlice("items")
		filter, _ := ctx.GetStringFlag("filter")
		sortBy, _ := ctx.GetStringFlag("sort")
		format, _ := ctx.GetStringFlag("format")
		limit, _ := ctx.GetIntFlag("limit")

		// Process items
		var processedItems []map[string]interface{}
		for i, item := range items {
			// Apply filter if specified
			if filter != "" && !strings.Contains(strings.ToLower(item), strings.ToLower(filter)) {
				continue
			}

			processedItems = append(processedItems, map[string]interface{}{
				"id":     i + 1,
				"name":   item,
				"length": len(item),
				"status": "active",
			})
		}

		// Apply sorting
		if sortBy == "name" {
			sort.Slice(processedItems, func(i, j int) bool {
				return processedItems[i]["name"].(string) < processedItems[j]["name"].(string)
			})
		}

		// Apply limit
		if limit > 0 && len(processedItems) > limit {
			processedItems = processedItems[:limit]
		}

		// Output based on format
		switch format {
		case "table":
			headers := []string{"ID", "Name", "Length", "Status"}
			var rows [][]string
			for _, item := range processedItems {
				rows = append(rows, []string{
					fmt.Sprintf("%d", item["id"]),
					item["name"].(string),
					fmt.Sprintf("%d", item["length"]),
					item["status"].(string),
				})
			}
			ctx.Table(headers, rows)
			return nil
		default:
			return ctx.OutputData(map[string]interface{}{
				"items":  processedItems,
				"total":  len(processedItems),
				"filter": filter,
				"sort":   sortBy,
			})
		}
	}

	return cmd
}

// createInteractiveCommand creates an interactive command
func createInteractiveCommand() *cli.Command {
	cmd := cli.NewCommand("interactive", "Interactive command example")

	cmd.WithLong("Demonstrates interactive prompts and user input").
		WithExample("basic-cli interactive")

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		ctx.InfoMsg("Welcome to the interactive command!")

		// Get user's name
		name, err := ctx.Input("What's your name?", nil)
		if err != nil {
			return fmt.Errorf("failed to get name: %w", err)
		}

		// Get user's age
		ageStr, err := ctx.Input("What's your age?", func(input string) error {
			if _, err := strconv.Atoi(input); err != nil {
				return fmt.Errorf("age must be a number")
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to get age: %w", err)
		}

		age, _ := strconv.Atoi(ageStr)

		// Select favorite color
		colors := []string{"Red", "Green", "Blue", "Yellow", "Purple"}
		colorIndex, err := ctx.Select("What's your favorite color?", colors)
		if err != nil {
			return fmt.Errorf("failed to get color: %w", err)
		}

		// Confirm information
		ctx.InfoMsg(fmt.Sprintf("You are %s, %d years old, and like %s", name, age, colors[colorIndex]))

		if ctx.Confirm("Is this information correct?") {
			ctx.Success("Information confirmed!")
		} else {
			ctx.Warning("Information not confirmed. Please run the command again.")
		}

		// Output structured data
		result := map[string]interface{}{
			"name":           name,
			"age":            age,
			"favorite_color": colors[colorIndex],
			"confirmed":      ctx.Confirm("Is this information correct?"),
		}

		return ctx.OutputData(result)
	}

	return cmd
}

package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// prompt reads a line of input from the user
func prompt(question string) (string, error) {
	fmt.Print(Blue(question) + " ")

	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(answer), nil
}

// confirm asks a yes/no question and returns true if yes
func confirm(question string) (bool, error) {
	fmt.Print(Blue(question) + " " + Gray("(y/n)") + " ")

	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}

	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes", nil
}

// selectPrompt presents a list of options and returns the selected one
// Supports arrow key navigation
func selectPrompt(question string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no options provided")
	}

	// Try interactive mode with arrow keys
	result, err := interactiveSelect(question, options, false)
	if err == nil && len(result) > 0 {
		return result[0], nil
	}

	// Fallback to number input if interactive mode fails
	return numberSelectPrompt(question, options)
}

// numberSelectPrompt is the fallback number-based selection
func numberSelectPrompt(question string, options []string) (string, error) {
	// Display options in a more visually appealing way
	fmt.Println(Bold(Blue(question)))
	fmt.Println(Gray("───────────────────────────────────────"))

	for i, opt := range options {
		fmt.Printf("  %s %d%s %s\n",
			Cyan("►"),
			i+1,
			Gray("."),
			opt)
	}

	fmt.Println(Gray("───────────────────────────────────────"))
	fmt.Print(Gray("Enter your choice (1-") + Gray(fmt.Sprintf("%d", len(options))) + Gray("): "))

	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	answer = strings.TrimSpace(answer)

	// Handle empty input
	if answer == "" {
		return "", fmt.Errorf("no selection made")
	}

	var selection int
	_, err = fmt.Sscanf(answer, "%d", &selection)
	if err != nil {
		return "", fmt.Errorf("invalid input: please enter a number between 1 and %d", len(options))
	}

	if selection < 1 || selection > len(options) {
		return "", fmt.Errorf("selection out of range: please choose between 1 and %d", len(options))
	}

	fmt.Println(Green("✓") + " Selected: " + Bold(options[selection-1]))
	return options[selection-1], nil
}

// multiSelectPrompt presents a list of options and returns the selected ones
// Supports arrow keys and space bar for selection
func multiSelectPrompt(question string, options []string) ([]string, error) {
	if len(options) == 0 {
		return nil, fmt.Errorf("no options provided")
	}

	// Try interactive mode with arrow keys and space
	results, err := interactiveSelect(question, options, true)
	if err == nil {
		return results, nil
	}

	// Fallback to number input if interactive mode fails
	return numberMultiSelectPrompt(question, options)
}

// numberMultiSelectPrompt is the fallback number-based multi-selection
func numberMultiSelectPrompt(question string, options []string) ([]string, error) {
	// Display options in a more visually appealing way
	fmt.Println(Bold(Blue(question)))
	fmt.Println(Gray("───────────────────────────────────────"))

	for i, opt := range options {
		fmt.Printf("  %s %d%s %s\n",
			Cyan("☐"),
			i+1,
			Gray("."),
			opt)
	}

	fmt.Println(Gray("───────────────────────────────────────"))
	fmt.Println(Gray("Enter choices separated by comma (e.g., 1,3,4)"))
	fmt.Println(Gray("Or enter ranges (e.g., 1-3,5,7-9)"))
	fmt.Print(Gray("Your selection (or press Enter for none): "))

	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}

	answer = strings.TrimSpace(answer)
	if answer == "" {
		fmt.Println(Gray("✓ No selections made"))
		return []string{}, nil
	}

	results := []string{}
	seen := make(map[int]bool) // Track to avoid duplicates

	// Split by comma
	selections := strings.Split(answer, ",")

	for _, sel := range selections {
		sel = strings.TrimSpace(sel)

		// Check if it's a range (e.g., "1-3")
		if strings.Contains(sel, "-") {
			parts := strings.Split(sel, "-")
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid range format: %s (expected format: 1-3)", sel)
			}

			var start, end int
			_, err1 := fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &start)
			_, err2 := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &end)

			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("invalid range: %s", sel)
			}

			if start < 1 || end > len(options) || start > end {
				return nil, fmt.Errorf("range out of bounds: %s (valid range: 1-%d)", sel, len(options))
			}

			// Add all items in range
			for i := start; i <= end; i++ {
				if !seen[i] {
					results = append(results, options[i-1])
					seen[i] = true
				}
			}
		} else {
			// Single selection
			var idx int
			_, err := fmt.Sscanf(sel, "%d", &idx)
			if err != nil {
				return nil, fmt.Errorf("invalid input: '%s' (expected a number or range)", sel)
			}

			if idx < 1 || idx > len(options) {
				return nil, fmt.Errorf("selection out of range: %d (valid range: 1-%d)", idx, len(options))
			}

			if !seen[idx] {
				results = append(results, options[idx-1])
				seen[idx] = true
			}
		}
	}

	// Display selected items
	if len(results) > 0 {
		fmt.Println(Green("✓") + " Selected " + Bold(fmt.Sprintf("%d", len(results))) + " item(s):")
		for _, item := range results {
			fmt.Println(Gray("  • ") + item)
		}
	}

	return results, nil
}

// interactiveSelect provides arrow key navigation for selection
func interactiveSelect(question string, options []string, multi bool) ([]string, error) {
	// Try to enable raw mode for terminal
	oldState, err := setRawMode()
	if err != nil {
		// Can't use raw mode, return error to fall back to number input
		return nil, err
	}
	defer restoreMode(oldState)

	cursor := 0
	selected := make(map[int]bool)
	firstRender := true

	// Hide cursor for cleaner UI
	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h") // Show cursor when done

	// Print initial UI - these lines should stay fixed
	// In raw mode, we need \r\n instead of just \n for proper line breaks
	fmt.Print(Bold(Blue(question)))
	fmt.Print("\r\n") // CR+LF after question
	fmt.Print(Gray("↑/↓: Navigate  │  "))
	if multi {
		fmt.Print(Gray("Space: Select/Deselect  │  Enter: Confirm  │  Esc/q: Cancel"))
	} else {
		fmt.Print(Gray("Enter: Select  │  Esc/q: Cancel"))
	}
	fmt.Print("\r\n") // CR+LF after instructions
	fmt.Print("\r\n") // Empty line before options
	os.Stdout.Sync()  // Flush header before rendering options

	for {
		// Render options
		renderOptions(options, cursor, selected, multi, firstRender)
		firstRender = false

		// Read single character
		buf := make([]byte, 3) // Need 3 bytes for arrow keys (ESC [ A/B)
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return nil, err
		}

		char := buf[0]

		// Handle input
		switch char {
		case 3: // Ctrl+C
			clearOptions(len(options))
			fmt.Print("\r\n")
			fmt.Print(Yellow("✗ Interrupted") + "\r\n")
			return nil, fmt.Errorf("interrupted by user")

		case 27: // ESC sequence
			// Arrow keys send ESC [ A/B/C/D
			if n >= 3 && buf[1] == '[' {
				switch buf[2] {
				case 'A': // Up arrow
					if cursor > 0 {
						cursor--
					}
				case 'B': // Down arrow
					if cursor < len(options)-1 {
						cursor++
					}
				}
			} else {
				// ESC pressed alone - cancel
				clearOptions(len(options))
				fmt.Print("\r\n")
				fmt.Print(Yellow("✗ Cancelled") + "\r\n")
				return nil, fmt.Errorf("cancelled")
			}

		case ' ': // Space (for multi-select)
			if multi {
				selected[cursor] = !selected[cursor]
			}

		case '\r', '\n': // Enter
			clearOptions(len(options))

			if multi {
				results := []string{}
				for i := range options {
					if selected[i] {
						results = append(results, options[i])
					}
				}

				if len(results) > 0 {
					fmt.Print(Green("✓") + " Selected " + Bold(fmt.Sprintf("%d", len(results))) + " item(s):\r\n")
					for _, item := range results {
						fmt.Print(Gray("  • ") + item + "\r\n")
					}
				} else {
					fmt.Print(Gray("✓ No selections made") + "\r\n")
				}

				return results, nil
			}

			fmt.Print(Green("✓") + " Selected: " + Bold(options[cursor]) + "\r\n")
			return []string{options[cursor]}, nil

		case 'q', 'Q': // Quit
			clearOptions(len(options))
			fmt.Print("\r\n")
			fmt.Print(Yellow("✗ Cancelled") + "\r\n")
			return nil, fmt.Errorf("cancelled")

		case 'j': // Vim-style down
			if cursor < len(options)-1 {
				cursor++
			}

		case 'k': // Vim-style up
			if cursor > 0 {
				cursor--
			}
		}
	}
}

// renderOptions renders the option list with current cursor position
func renderOptions(options []string, cursor int, selected map[int]bool, multi bool, firstRender bool) {
	// Clear previous render (skip on first render)
	if !firstRender {
		// Move cursor up to the first line of options
		for i := 0; i < len(options); i++ {
			fmt.Print("\033[1A") // Move up one line
		}
	}

	// Render each option (this overwrites the previous render)
	for i, opt := range options {
		// Clear the entire line first - use \033[2K to clear entire line, then \r to return to start
		fmt.Print("\033[2K\r")

		prefix := "  "
		optText := opt

		if i == cursor {
			// Highlighted item
			if multi && selected[i] {
				prefix = Green("▸ [✓]")
			} else if multi {
				prefix = Green("▸ [ ]")
			} else {
				prefix = Green("▸")
			}
			optText = Bold(opt)
		} else if multi && selected[i] {
			prefix = "  [✓]"
		} else if multi {
			prefix = "  [ ]"
		}

		fmt.Printf("%s %s\r\n", prefix, optText)
	}

	// Flush output to ensure immediate display
	os.Stdout.Sync()
}

// clearOptions clears n lines from the terminal
func clearOptions(n int) {
	// Move up n lines
	for i := 0; i < n; i++ {
		fmt.Print("\033[1A") // Move up one line
	}
	// Clear each line from current position down
	for i := 0; i < n; i++ {
		fmt.Print("\033[2K\r") // Clear entire line and return to start
		if i < n-1 {
			fmt.Print("\r\n") // Move to next line (except last)
		}
	}
	// Move back up to start
	for i := 0; i < n-1; i++ {
		fmt.Print("\033[1A")
	}
	fmt.Print("\r") // Return to start of first line
}

// setRawMode enables raw mode on the terminal
func setRawMode() (*term.State, error) {
	// Get stdin file descriptor
	fd := int(os.Stdin.Fd())

	// Check if stdin is a terminal
	if !term.IsTerminal(fd) {
		return nil, fmt.Errorf("not a terminal")
	}

	// Save current terminal state
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}

	return oldState, nil
}

// restoreMode restores the terminal to normal mode
func restoreMode(oldState *term.State) {
	if oldState != nil {
		term.Restore(int(os.Stdin.Fd()), oldState)
	}
}

// PromptWithDefault prompts with a default value
func PromptWithDefault(question, defaultValue string) (string, error) {
	fmt.Print(Blue(question) + " " + Gray(fmt.Sprintf("[%s]", defaultValue)) + " ")

	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	answer = strings.TrimSpace(answer)
	if answer == "" {
		return defaultValue, nil
	}

	return answer, nil
}

// PromptPassword prompts for a password (hidden input)
func PromptPassword(question string) (string, error) {
	fmt.Print(Blue(question) + " ")

	// Read password without echo
	passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}

	fmt.Println() // Add newline after password input

	return string(passwordBytes), nil
}

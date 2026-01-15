package cli

import (
	"context"
	"fmt"
	"os"
	"time"
)

// OptionsLoader is a function that loads options asynchronously.
type OptionsLoader func(ctx context.Context) ([]string, error)

// SelectAsync prompts for selection with async-loaded options
// Shows a spinner while loading options.
func SelectAsync(ctx context.Context, question string, loader OptionsLoader) (string, error) {
	// Start spinner
	spinner := newSpinner("Loading options...", os.Stdout)

	// Load options in background
	type result struct {
		options []string
		err     error
	}

	resultCh := make(chan result, 1)

	go func() {
		opts, err := loader(ctx)
		resultCh <- result{options: opts, err: err}
	}()

	// Wait for options or context cancellation
	var options []string

	select {
	case <-ctx.Done():
		spinner.Stop(Red("✗ Cancelled"))

		return "", ctx.Err()
	case res := <-resultCh:
		if res.err != nil {
			spinner.Stop(Red("✗ Failed to load options"))

			return "", fmt.Errorf("failed to load options: %w", res.err)
		}

		options = res.options

		spinner.Stop(Green("✓ Options loaded"))
	}

	// Give user a moment to see the success message
	time.Sleep(300 * time.Millisecond)

	// Now prompt for selection
	return selectPrompt(question, options)
}

// MultiSelectAsync prompts for multiple selections with async-loaded options
// Shows a spinner while loading options.
func MultiSelectAsync(ctx context.Context, question string, loader OptionsLoader) ([]string, error) {
	// Start spinner
	spinner := newSpinner("Loading options...", os.Stdout)

	// Load options in background
	type result struct {
		options []string
		err     error
	}

	resultCh := make(chan result, 1)

	go func() {
		opts, err := loader(ctx)
		resultCh <- result{options: opts, err: err}
	}()

	// Wait for options or context cancellation
	var options []string

	select {
	case <-ctx.Done():
		spinner.Stop(Red("✗ Cancelled"))

		return nil, ctx.Err()
	case res := <-resultCh:
		if res.err != nil {
			spinner.Stop(Red("✗ Failed to load options"))

			return nil, fmt.Errorf("failed to load options: %w", res.err)
		}

		options = res.options

		spinner.Stop(Green("✓ Options loaded"))
	}

	// Give user a moment to see the success message
	time.Sleep(300 * time.Millisecond)

	// Now prompt for selection
	return multiSelectPrompt(question, options)
}

// SelectWithRetry prompts for selection with retry on failure
// Useful for loading from flaky sources.
func SelectWithRetry(ctx context.Context, question string, loader OptionsLoader, maxRetries int) (string, error) {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		spinner := newSpinner(fmt.Sprintf("Loading options (attempt %d/%d)...", attempt, maxRetries), os.Stdout)

		options, err := loader(ctx)
		if err == nil {
			spinner.Stop(Green("✓ Options loaded"))
			time.Sleep(300 * time.Millisecond)

			return selectPrompt(question, options)
		}

		lastErr = err

		spinner.Stop(Yellow(fmt.Sprintf("⚠ Attempt %d failed, retrying...", attempt)))

		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt) * time.Second) // Exponential backoff
		}
	}

	return "", fmt.Errorf("failed to load options after %d attempts: %w", maxRetries, lastErr)
}

// MultiSelectWithRetry prompts for multiple selections with retry on failure.
func MultiSelectWithRetry(ctx context.Context, question string, loader OptionsLoader, maxRetries int) ([]string, error) {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		spinner := newSpinner(fmt.Sprintf("Loading options (attempt %d/%d)...", attempt, maxRetries), os.Stdout)

		options, err := loader(ctx)
		if err == nil {
			spinner.Stop(Green("✓ Options loaded"))
			time.Sleep(300 * time.Millisecond)

			return multiSelectPrompt(question, options)
		}

		lastErr = err

		spinner.Stop(Yellow(fmt.Sprintf("⚠ Attempt %d failed, retrying...", attempt)))

		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt) * time.Second) // Exponential backoff
		}
	}

	return nil, fmt.Errorf("failed to load options after %d attempts: %w", maxRetries, lastErr)
}

// ProgressLoader prompts for selection with progress feedback.
// Useful when loading takes multiple steps.
type ProgressLoader func(ctx context.Context, progress func(current, total int, message string)) ([]string, error)

// SelectWithProgressFeedback prompts with detailed progress feedback.
func SelectWithProgressFeedback(ctx context.Context, question string, loader ProgressLoader) (string, error) {
	var currentMsg string

	spinner := newSpinner("Starting...", os.Stdout)

	updateProgress := func(current, total int, message string) {
		currentMsg = fmt.Sprintf("%s (%d/%d)", message, current, total)
		spinner.Update(currentMsg)
	}

	options, err := loader(ctx, updateProgress)
	if err != nil {
		spinner.Stop(Red("✗ Failed: " + err.Error()))

		return "", err
	}

	spinner.Stop(Green("✓ Ready"))
	time.Sleep(300 * time.Millisecond)

	return selectPrompt(question, options)
}

// MultiSelectWithProgressFeedback prompts for multiple selections with progress feedback.
func MultiSelectWithProgressFeedback(ctx context.Context, question string, loader ProgressLoader) ([]string, error) {
	var currentMsg string

	spinner := newSpinner("Starting...", os.Stdout)

	updateProgress := func(current, total int, message string) {
		currentMsg = fmt.Sprintf("%s (%d/%d)", message, current, total)
		spinner.Update(currentMsg)
	}

	options, err := loader(ctx, updateProgress)
	if err != nil {
		spinner.Stop(Red("✗ Failed: " + err.Error()))

		return nil, err
	}

	spinner.Stop(Green("✓ Ready"))
	time.Sleep(300 * time.Millisecond)

	return multiSelectPrompt(question, options)
}

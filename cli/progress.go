package cli

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// ProgressBar displays a progress bar
type ProgressBar interface {
	Set(current int)
	Increment()
	Finish(message string)
}

// progressBar implements ProgressBar
type progressBar struct {
	total   int
	current int
	output  io.Writer
	width   int
	mu      sync.Mutex
	done    bool
}

// newProgressBar creates a new progress bar
func newProgressBar(total int, output io.Writer) ProgressBar {
	return &progressBar{
		total:  total,
		output: output,
		width:  40,
	}
}

// Set sets the current progress value
func (pb *progressBar) Set(current int) {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	if pb.done {
		return
	}

	pb.current = current
	if pb.current > pb.total {
		pb.current = pb.total
	}

	pb.render()
}

// Increment increments the progress by 1
func (pb *progressBar) Increment() {
	pb.Set(pb.current + 1)
}

// Finish completes the progress bar with a message
func (pb *progressBar) Finish(message string) {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	if pb.done {
		return
	}

	pb.done = true
	pb.current = pb.total
	pb.render()
	fmt.Fprintln(pb.output)

	if message != "" {
		fmt.Fprintln(pb.output, Green(message))
	}
}

// render draws the progress bar
func (pb *progressBar) render() {
	percent := float64(pb.current) / float64(pb.total)
	if percent > 1.0 {
		percent = 1.0
	}

	filled := int(percent * float64(pb.width))
	empty := pb.width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	percentStr := fmt.Sprintf("%3.0f%%", percent*100)

	// Clear line and redraw
	fmt.Fprintf(pb.output, "\r%s [%s] %s", Gray("Progress:"), bar, percentStr)
}

// Spinner displays a spinner animation
type Spinner interface {
	Update(message string)
	Stop(message string)
}

// spinner implements Spinner
type spinner struct {
	message string
	output  io.Writer
	frames  []string
	index   int
	done    bool
	mu      sync.Mutex
	ticker  *time.Ticker
	stopCh  chan struct{}
}

// newSpinner creates a new spinner
func newSpinner(message string, output io.Writer) Spinner {
	s := &spinner{
		message: message,
		output:  output,
		frames:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		stopCh:  make(chan struct{}),
	}

	// Start animation
	s.ticker = time.NewTicker(80 * time.Millisecond)
	go s.animate()

	return s
}

// animate runs the spinner animation
func (s *spinner) animate() {
	for {
		select {
		case <-s.stopCh:
			return
		case <-s.ticker.C:
			s.mu.Lock()
			if !s.done {
				s.render()
				s.index = (s.index + 1) % len(s.frames)
			}
			s.mu.Unlock()
		}
	}
}

// render draws the spinner
func (s *spinner) render() {
	frame := s.frames[s.index]
	fmt.Fprintf(s.output, "\r%s %s", Cyan(frame), s.message)
}

// Update updates the spinner message
func (s *spinner) Update(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done {
		return
	}

	s.message = message
}

// Stop stops the spinner with a final message
func (s *spinner) Stop(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done {
		return
	}

	s.done = true
	s.ticker.Stop()
	close(s.stopCh)

	// Clear the spinner line
	fmt.Fprintf(s.output, "\r%s\r", strings.Repeat(" ", 80))

	// Print final message
	if message != "" {
		fmt.Fprintln(s.output, message)
	}
}

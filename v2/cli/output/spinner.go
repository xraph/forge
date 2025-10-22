package output

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
)

// ProgressTracker defines the interface for progress tracking
type ProgressTracker interface {
	Update(current int, description string)
	Increment(description string)
	Finish(message string)
	Stop()
	SetTotal(total int)
	GetCurrent() int
	GetTotal() int
	IsFinished() bool
}

// ModernProgressBar wraps progressbar/v3 to implement ProgressTracker
type ModernProgressBar struct {
	bar      *progressbar.ProgressBar
	writer   io.Writer
	finished bool
	mutex    sync.RWMutex
}

// ModernSpinner wraps progressbar/v3 spinner to implement ProgressTracker
type ModernSpinner struct {
	bar      *progressbar.ProgressBar
	writer   io.Writer
	finished bool
	mutex    sync.RWMutex
}

// ProgressBarConfig contains progress bar configuration
type ProgressBarConfig struct {
	Width       int    `yaml:"width" json:"width"`
	ShowPercent bool   `yaml:"show_percent" json:"show_percent"`
	ShowETA     bool   `yaml:"show_eta" json:"show_eta"`
	ShowRate    bool   `yaml:"show_rate" json:"show_rate"`
	ShowCount   bool   `yaml:"show_count" json:"show_count"`
	Theme       string `yaml:"theme" json:"theme"` // default, unicode, ascii
	Description string `yaml:"description" json:"description"`
}

// SpinnerConfig contains spinner configuration
type SpinnerConfig struct {
	SpinnerType int           `yaml:"spinner_type" json:"spinner_type"`
	Interval    time.Duration `yaml:"interval" json:"interval"`
	Description string        `yaml:"description" json:"description"`
	ShowElapsed bool          `yaml:"show_elapsed" json:"show_elapsed"`
}

// NewModernProgressBar creates a new progress bar using progressbar/v3
func NewModernProgressBar(writer io.Writer, total int, config ...ProgressBarConfig) ProgressTracker {
	var cfg ProgressBarConfig
	if len(config) > 0 {
		cfg = config[0]
	} else {
		cfg = DefaultProgressBarConfig()
	}

	var options []progressbar.Option

	// Basic options
	options = append(options,
		progressbar.OptionSetWriter(writer),
		progressbar.OptionSetWidth(cfg.Width),
		progressbar.OptionSetDescription(cfg.Description),
		progressbar.OptionEnableColorCodes(true),
	)

	// Conditional options
	if cfg.ShowPercent {
		options = append(options, progressbar.OptionShowElapsedTimeOnFinish())
	}

	if cfg.ShowCount {
		options = append(options, progressbar.OptionShowCount())
	}

	if cfg.ShowETA {
		options = append(options, progressbar.OptionShowElapsedTimeOnFinish())
	}

	// Theme options
	switch cfg.Theme {
	case "unicode":
		options = append(options,
			progressbar.OptionUseANSICodes(true),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "█",
				SaucerHead:    "█",
				SaucerPadding: "░",
				BarStart:      "▐",
				BarEnd:        "▌",
			}),
		)
	case "ascii":
		options = append(options,
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "=",
				SaucerHead:    ">",
				SaucerPadding: " ",
				BarStart:      "[",
				BarEnd:        "]",
			}),
		)
	default:
		options = append(options, progressbar.OptionUseANSICodes(true))
	}

	// Completion callback
	options = append(options, progressbar.OptionOnCompletion(func() {
		fmt.Fprint(writer, "\n")
	}))

	bar := progressbar.NewOptions(total, options...)

	return &ModernProgressBar{
		bar:      bar,
		writer:   writer,
		finished: false,
	}
}

// NewModernSpinner creates a new spinner using progressbar/v3
func NewModernSpinner(writer io.Writer, config ...SpinnerConfig) ProgressTracker {
	var cfg SpinnerConfig
	if len(config) > 0 {
		cfg = config[0]
	} else {
		cfg = DefaultSpinnerConfig()
	}

	options := []progressbar.Option{
		progressbar.OptionSetWriter(writer),
		progressbar.OptionSetDescription(cfg.Description),
		progressbar.OptionSpinnerType(cfg.SpinnerType),
		progressbar.OptionShowCount(),
		progressbar.OptionThrottle(cfg.Interval),
	}

	if cfg.ShowElapsed {
		options = append(options, progressbar.OptionShowElapsedTimeOnFinish())
	}

	// Completion callback
	options = append(options, progressbar.OptionOnCompletion(func() {
		fmt.Fprint(writer, "\n")
	}))

	// -1 for indeterminate progress (spinner)
	bar := progressbar.NewOptions(-1, options...)

	return &ModernSpinner{
		bar:      bar,
		writer:   writer,
		finished: false,
	}
}

// ModernProgressBar implementation

// Update updates the progress bar
func (mpb *ModernProgressBar) Update(current int, description string) {
	mpb.mutex.Lock()
	defer mpb.mutex.Unlock()

	if mpb.finished {
		return
	}

	if description != "" {
		mpb.bar.Describe(description)
	}
	mpb.bar.Set(current)
}

// Increment increments the progress bar
func (mpb *ModernProgressBar) Increment(description string) {
	mpb.mutex.Lock()
	defer mpb.mutex.Unlock()

	if mpb.finished {
		return
	}

	if description != "" {
		mpb.bar.Describe(description)
	}
	mpb.bar.Add(1)
}

// Finish completes the progress bar
func (mpb *ModernProgressBar) Finish(message string) {
	mpb.mutex.Lock()
	defer mpb.mutex.Unlock()

	if mpb.finished {
		return
	}

	mpb.finished = true
	if message != "" {
		mpb.bar.Describe(message)
	}
	mpb.bar.Finish()
}

// Stop stops the progress bar
func (mpb *ModernProgressBar) Stop() {
	mpb.mutex.Lock()
	defer mpb.mutex.Unlock()

	if mpb.finished {
		return
	}

	mpb.finished = true
	mpb.bar.Exit()
}

// SetTotal sets the total value
func (mpb *ModernProgressBar) SetTotal(total int) {
	mpb.mutex.Lock()
	defer mpb.mutex.Unlock()

	if !mpb.finished {
		mpb.bar.ChangeMax(total)
	}
}

// GetCurrent returns the current progress
func (mpb *ModernProgressBar) GetCurrent() int {
	mpb.mutex.RLock()
	defer mpb.mutex.RUnlock()
	return int(mpb.bar.State().CurrentNum)
}

// GetTotal returns the total value
func (mpb *ModernProgressBar) GetTotal() int {
	mpb.mutex.RLock()
	defer mpb.mutex.RUnlock()
	return mpb.bar.GetMax()
}

// IsFinished returns whether the progress bar is finished
func (mpb *ModernProgressBar) IsFinished() bool {
	mpb.mutex.RLock()
	defer mpb.mutex.RUnlock()
	return mpb.finished
}

// ModernSpinner implementation

// Update updates the spinner description
func (ms *ModernSpinner) Update(current int, description string) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if ms.finished {
		return
	}

	if description != "" {
		ms.bar.Describe(description)
	}
	ms.bar.Add(1) // Keep the spinner moving
}

// Increment increments the spinner (just updates description and spins)
func (ms *ModernSpinner) Increment(description string) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if ms.finished {
		return
	}

	if description != "" {
		ms.bar.Describe(description)
	}
	ms.bar.Add(1)
}

// Finish completes the spinner
func (ms *ModernSpinner) Finish(message string) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if ms.finished {
		return
	}

	ms.finished = true
	if message != "" {
		ms.bar.Describe(message)
	}
	ms.bar.Finish()
}

// Stop stops the spinner
func (ms *ModernSpinner) Stop() {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if ms.finished {
		return
	}

	ms.finished = true
	ms.bar.Exit()
}

// SetTotal does nothing for spinners (indeterminate progress)
func (ms *ModernSpinner) SetTotal(total int) {
	// Spinners don't have a total
}

// GetCurrent returns 0 for spinners
func (ms *ModernSpinner) GetCurrent() int {
	return 0
}

// GetTotal returns 0 for spinners
func (ms *ModernSpinner) GetTotal() int {
	return 0
}

// IsFinished returns whether the spinner is finished
func (ms *ModernSpinner) IsFinished() bool {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()
	return ms.finished
}

// BuildProgressTracker creates an appropriate progress tracker for build operations
func NewBuildProgressTracker(writer io.Writer, taskName string) ProgressTracker {
	config := SpinnerConfig{
		SpinnerType: 14, // dots
		Interval:    80 * time.Millisecond,
		Description: fmt.Sprintf("Building %s", taskName),
		ShowElapsed: true,
	}

	return NewModernSpinner(writer, config)
}

// DownloadProgressTracker creates a progress bar suitable for downloads
func NewDownloadProgressTracker(writer io.Writer, total int, filename string) ProgressTracker {
	config := ProgressBarConfig{
		Width:       50,
		ShowPercent: true,
		ShowETA:     true,
		ShowRate:    true,
		ShowCount:   true,
		Theme:       "unicode",
		Description: fmt.Sprintf("Downloading %s", filename),
	}

	return NewModernProgressBar(writer, total, config)
}

// MultiStepProgressTracker creates a progress bar for multi-step operations
func NewMultiStepProgressTracker(writer io.Writer, totalSteps int, operation string) ProgressTracker {
	config := ProgressBarConfig{
		Width:       40,
		ShowPercent: true,
		ShowETA:     false,
		ShowRate:    false,
		ShowCount:   true,
		Theme:       "default",
		Description: operation,
	}

	return NewModernProgressBar(writer, totalSteps, config)
}

// Default configurations

// DefaultProgressBarConfig returns default progress bar configuration
func DefaultProgressBarConfig() ProgressBarConfig {
	return ProgressBarConfig{
		Width:       50,
		ShowPercent: true,
		ShowETA:     true,
		ShowRate:    false,
		ShowCount:   false,
		Theme:       "default",
		Description: "Progress",
	}
}

// DefaultSpinnerConfig returns default spinner configuration
func DefaultSpinnerConfig() SpinnerConfig {
	return SpinnerConfig{
		SpinnerType: 14, // dots spinner
		Interval:    80 * time.Millisecond,
		Description: "Processing",
		ShowElapsed: true,
	}
}

// Preset spinner types for different use cases
const (
	SpinnerDots         = 14 // ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏
	SpinnerLine         = 11 // |/-\
	SpinnerArrow        = 4  // ←↖↑↗→↘↓↙
	SpinnerBouncingBar  = 5  // ▉▊▋▌▍▎▏▎▍▌▋▊▉
	SpinnerBouncingBall = 6  // ⠁⠂⠄⠂
	SpinnerClock        = 12 // 🕐🕑🕒🕓🕔🕕🕖🕗🕘🕙🕚🕛
)

// Utility functions for backward compatibility with existing progress.go

// ProgressBar implements a text-based progress bar (legacy wrapper)
type ProgressBar struct {
	modernBar ProgressTracker
}

// NewProgressBar creates a new progress bar (legacy interface)
func NewProgressBar(writer io.Writer, total int) ProgressTracker {
	return NewModernProgressBar(writer, total)
}

// NewProgressBarWithConfig creates a progress bar with custom configuration (legacy interface)
func NewProgressBarWithConfig(writer io.Writer, total int, config ProgressBarConfig) ProgressTracker {
	return NewModernProgressBar(writer, total, config)
}

// Spinner implements a spinning progress indicator (legacy wrapper)
type Spinner struct {
	modernSpinner ProgressTracker
}

// NewSpinner creates a new spinner (legacy interface)
func NewSpinner(writer io.Writer, description string) ProgressTracker {
	config := SpinnerConfig{
		SpinnerType: SpinnerDots,
		Description: description,
		ShowElapsed: true,
	}
	return NewModernSpinner(writer, config)
}

// NewSpinnerWithConfig creates a spinner with custom configuration (legacy interface)
func NewSpinnerWithConfig(writer io.Writer, config SpinnerConfig) ProgressTracker {
	return NewModernSpinner(writer, config)
}

// MultiProgressTracker tracks multiple progress bars
type MultiProgressTracker struct {
	writer   io.Writer
	trackers map[string]ProgressTracker
	finished bool
	mutex    sync.RWMutex
}

// NewMultiProgressTracker creates a new multi-progress tracker
func NewMultiProgressTracker(writer io.Writer) *MultiProgressTracker {
	return &MultiProgressTracker{
		writer:   writer,
		trackers: make(map[string]ProgressTracker),
		finished: false,
	}
}

// AddTracker adds a progress tracker
func (mpt *MultiProgressTracker) AddTracker(name string, tracker ProgressTracker) {
	mpt.mutex.Lock()
	defer mpt.mutex.Unlock()

	if !mpt.finished {
		mpt.trackers[name] = tracker
	}
}

// RemoveTracker removes a progress tracker
func (mpt *MultiProgressTracker) RemoveTracker(name string) {
	mpt.mutex.Lock()
	defer mpt.mutex.Unlock()

	if tracker, exists := mpt.trackers[name]; exists {
		tracker.Stop()
		delete(mpt.trackers, name)
	}
}

// FinishAll finishes all trackers
func (mpt *MultiProgressTracker) FinishAll() {
	mpt.mutex.Lock()
	defer mpt.mutex.Unlock()

	for _, tracker := range mpt.trackers {
		tracker.Stop()
	}
	mpt.finished = true
}

// GetTracker returns a tracker by name
func (mpt *MultiProgressTracker) GetTracker(name string) (ProgressTracker, bool) {
	mpt.mutex.RLock()
	defer mpt.mutex.RUnlock()

	tracker, exists := mpt.trackers[name]
	return tracker, exists
}

// ProgressIndicator combines multiple progress tracking methods
type ProgressIndicator struct {
	tracker     ProgressTracker
	showSpinner bool
	spinner     ProgressTracker
}

// NewProgressIndicator creates a new progress indicator
func NewProgressIndicator(writer io.Writer, total int, showSpinner bool) *ProgressIndicator {
	var tracker ProgressTracker

	if total > 0 {
		tracker = NewModernProgressBar(writer, total)
	} else {
		tracker = NewModernSpinner(writer)
	}

	var spinner ProgressTracker
	if showSpinner && total > 0 {
		spinner = NewModernSpinner(writer)
	}

	return &ProgressIndicator{
		tracker:     tracker,
		showSpinner: showSpinner,
		spinner:     spinner,
	}
}

// Update updates the progress indicator
func (pi *ProgressIndicator) Update(current int, description string) {
	pi.tracker.Update(current, description)
	if pi.spinner != nil {
		pi.spinner.Update(0, description)
	}
}

// Finish finishes the progress indicator
func (pi *ProgressIndicator) Finish(message string) {
	if pi.spinner != nil {
		pi.spinner.Stop()
	}
	pi.tracker.Finish(message)
}

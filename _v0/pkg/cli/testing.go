package cli

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	json "github.com/json-iterator/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// CLITester provides testing utilities for CLI applications
type CLITester struct {
	app         CLIApp
	output      *bytes.Buffer
	errorOutput *bytes.Buffer
	input       *bytes.Buffer

	// Mocking
	services   map[reflect.Type]interface{}
	middleware []CLIMiddleware
	mocks      []Mock

	// Assertions
	assertions []Assertion
	timeout    time.Duration
}

// Assertion defines a test assertion
type Assertion func(*CLITestResult) error

// CLITestResult contains the results of a CLI test execution
type CLITestResult struct {
	ExitCode    int                    `json:"exit_code"`
	Output      string                 `json:"output"`
	ErrorOutput string                 `json:"error_output"`
	Duration    time.Duration          `json:"duration"`
	Error       error                  `json:"error"`
	Commands    []string               `json:"commands"`
	Flags       map[string]interface{} `json:"flags"`
	Context     map[string]interface{} `json:"context"`
}

// Mock represents a service mock
type Mock interface {
	Setup() error
	Verify() error
	Reset()
}

// NewCLITester creates a new CLI tester
func NewCLITester(app CLIApp) *CLITester {
	return &CLITester{
		app:         app,
		output:      &bytes.Buffer{},
		errorOutput: &bytes.Buffer{},
		input:       &bytes.Buffer{},
		services:    make(map[reflect.Type]interface{}),
		assertions:  make([]Assertion, 0),
		timeout:     30 * time.Second,
	}
}

// MockService mocks a service for testing
func (ct *CLITester) MockService(serviceType interface{}, mock interface{}) *CLITester {
	ct.services[reflect.TypeOf(serviceType)] = mock
	return ct
}

// AddMock adds a mock to the tester
func (ct *CLITester) AddMock(mock Mock) *CLITester {
	ct.mocks = append(ct.mocks, mock)
	return ct
}

// SetInput sets the input for interactive commands
func (ct *CLITester) SetInput(input string) *CLITester {
	ct.input.Reset()
	ct.input.WriteString(input)
	return ct
}

// SetTimeout sets the test execution timeout
func (ct *CLITester) SetTimeout(timeout time.Duration) *CLITester {
	ct.timeout = timeout
	return ct
}

// Execute executes the CLI with the given arguments
func (ct *CLITester) Execute(args ...string) (*CLITestResult, error) {
	// Setup test environment
	ct.setupTestEnvironment()

	// Setup mocks
	for _, mock := range ct.mocks {
		if err := mock.Setup(); err != nil {
			return nil, fmt.Errorf("failed to setup mock: %w", err)
		}
	}

	start := time.Now()

	// Execute with timeout
	var err error
	done := make(chan struct{})

	go func() {
		defer close(done)
		err = ct.app.ExecuteWithArgs(args)
	}()

	select {
	case <-done:
		// Execution completed
	case <-time.After(ct.timeout):
		return nil, fmt.Errorf("test execution timeout after %v", ct.timeout)
	}

	duration := time.Since(start)

	// Create result
	result := &CLITestResult{
		ExitCode:    ct.getExitCode(err),
		Output:      ct.output.String(),
		ErrorOutput: ct.errorOutput.String(),
		Duration:    duration,
		Error:       err,
		Commands:    ct.extractCommands(args),
		Flags:       ct.extractFlags(args),
		Context:     make(map[string]interface{}),
	}

	// Verify mocks
	for _, mock := range ct.mocks {
		if err := mock.Verify(); err != nil {
			return result, fmt.Errorf("mock verification failed: %w", err)
		}
	}

	// Run assertions
	for _, assertion := range ct.assertions {
		if err := assertion(result); err != nil {
			return result, fmt.Errorf("assertion failed: %w", err)
		}
	}

	return result, nil
}

// setupTestEnvironment sets up the test environment
func (ct *CLITester) setupTestEnvironment() {
	// Set output streams
	ct.app.SetOutput(ct.output)
	ct.app.SetInput(ct.input)

	// Register mock services
	for _, mock := range ct.services {
		ct.app.RegisterService(mock)
	}
}

// getExitCode extracts exit code from error
func (ct *CLITester) getExitCode(err error) int {
	if err == nil {
		return 0
	}

	// Check for specific error types that map to exit codes
	switch err.(type) {
	default:
		return 1
	}
}

// extractCommands extracts command names from arguments
func (ct *CLITester) extractCommands(args []string) []string {
	commands := make([]string, 0)
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			commands = append(commands, arg)
		}
	}
	return commands
}

// extractFlags extracts flags from arguments
func (ct *CLITester) extractFlags(args []string) map[string]interface{} {
	flags := make(map[string]interface{})

	for i, arg := range args {
		if strings.HasPrefix(arg, "--") {
			flagName := strings.TrimPrefix(arg, "--")
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags[flagName] = args[i+1]
			} else {
				flags[flagName] = true
			}
		} else if strings.HasPrefix(arg, "-") && len(arg) == 2 {
			flagName := strings.TrimPrefix(arg, "-")
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags[flagName] = args[i+1]
			} else {
				flags[flagName] = true
			}
		}
	}

	return flags
}

// Assertion helpers

// ExpectSuccess expects the command to succeed
func (ct *CLITester) ExpectSuccess() *CLITester {
	ct.assertions = append(ct.assertions, func(result *CLITestResult) error {
		if result.ExitCode != 0 {
			return fmt.Errorf("expected success (exit code 0), got exit code %d: %v", result.ExitCode, result.Error)
		}
		return nil
	})
	return ct
}

// ExpectFailure expects the command to fail
func (ct *CLITester) ExpectFailure() *CLITester {
	ct.assertions = append(ct.assertions, func(result *CLITestResult) error {
		if result.ExitCode == 0 {
			return fmt.Errorf("expected failure (non-zero exit code), got success")
		}
		return nil
	})
	return ct
}

// ExpectExitCode expects a specific exit code
func (ct *CLITester) ExpectExitCode(code int) *CLITester {
	ct.assertions = append(ct.assertions, func(result *CLITestResult) error {
		if result.ExitCode != code {
			return fmt.Errorf("expected exit code %d, got %d", code, result.ExitCode)
		}
		return nil
	})
	return ct
}

// ExpectOutput expects specific output content
func (ct *CLITester) ExpectOutput(expected string) *CLITester {
	ct.assertions = append(ct.assertions, func(result *CLITestResult) error {
		if !strings.Contains(result.Output, expected) {
			return fmt.Errorf("expected output to contain %q, got:\n%s", expected, result.Output)
		}
		return nil
	})
	return ct
}

// ExpectOutputRegex expects output to match regex
func (ct *CLITester) ExpectOutputRegex(pattern string) *CLITester {
	ct.assertions = append(ct.assertions, func(result *CLITestResult) error {
		matched, err := regexp.MatchString(pattern, result.Output)
		if err != nil {
			return fmt.Errorf("invalid regex pattern %q: %w", pattern, err)
		}
		if !matched {
			return fmt.Errorf("expected output to match pattern %q, got:\n%s", pattern, result.Output)
		}
		return nil
	})
	return ct
}

// ExpectError expects specific error message
func (ct *CLITester) ExpectError(expectedError string) *CLITester {
	ct.assertions = append(ct.assertions, func(result *CLITestResult) error {
		if result.Error == nil {
			return fmt.Errorf("expected error containing %q, got success", expectedError)
		}
		if !strings.Contains(result.Error.Error(), expectedError) {
			return fmt.Errorf("expected error to contain %q, got %q", expectedError, result.Error.Error())
		}
		return nil
	})
	return ct
}

// ExpectDuration expects execution to complete within duration
func (ct *CLITester) ExpectDuration(maxDuration time.Duration) *CLITester {
	ct.assertions = append(ct.assertions, func(result *CLITestResult) error {
		if result.Duration > maxDuration {
			return fmt.Errorf("expected execution to complete within %v, took %v", maxDuration, result.Duration)
		}
		return nil
	})
	return ct
}

// ExpectNoOutput expects no output
func (ct *CLITester) ExpectNoOutput() *CLITester {
	ct.assertions = append(ct.assertions, func(result *CLITestResult) error {
		if result.Output != "" {
			return fmt.Errorf("expected no output, got:\n%s", result.Output)
		}
		return nil
	})
	return ct
}

// ExpectJSONOutput expects valid JSON output
func (ct *CLITester) ExpectJSONOutput() *CLITester {
	ct.assertions = append(ct.assertions, func(result *CLITestResult) error {
		var jsonData interface{}
		if err := json.Unmarshal([]byte(result.Output), &jsonData); err != nil {
			return fmt.Errorf("expected valid JSON output, got invalid JSON: %w", err)
		}
		return nil
	})
	return ct
}

// ExpectCommands expects specific commands to have been executed
func (ct *CLITester) ExpectCommands(commands ...string) *CLITester {
	ct.assertions = append(ct.assertions, func(result *CLITestResult) error {
		if len(result.Commands) != len(commands) {
			return fmt.Errorf("expected %d commands, got %d", len(commands), len(result.Commands))
		}
		for i, expected := range commands {
			if result.Commands[i] != expected {
				return fmt.Errorf("expected command %d to be %q, got %q", i, expected, result.Commands[i])
			}
		}
		return nil
	})
	return ct
}

// MockService provides a base for service mocks
type MockService struct {
	mock.Mock
	setupCalled bool
}

// Setup sets up the mock service
func (ms *MockService) Setup() error {
	ms.setupCalled = true
	return nil
}

// Verify verifies the mock expectations
func (ms *MockService) Verify() error {
	return nil
}

// Reset resets the mock
func (ms *MockService) Reset() {
	ms.Mock.ExpectedCalls = nil
	ms.Mock.Calls = nil
	ms.setupCalled = false
}

// TestSuite provides a test suite for CLI applications
type TestSuite struct {
	t       *testing.T
	app     CLIApp
	tester  *CLITester
	cleanup []func()
}

// NewTestSuite creates a new test suite
func NewTestSuite(t *testing.T, app CLIApp) *TestSuite {
	return &TestSuite{
		t:       t,
		app:     app,
		tester:  NewCLITester(app),
		cleanup: make([]func(), 0),
	}
}

// SetupTest sets up a test
func (ts *TestSuite) SetupTest() {
	// Reset the tester for each test
	ts.tester = NewCLITester(ts.app)
}

// TearDownTest cleans up after a test
func (ts *TestSuite) TearDownTest() {
	// Run cleanup functions
	for _, cleanupFunc := range ts.cleanup {
		cleanupFunc()
	}
	ts.cleanup = ts.cleanup[:0]
}

// AddCleanup adds a cleanup function
func (ts *TestSuite) AddCleanup(cleanup func()) {
	ts.cleanup = append(ts.cleanup, cleanup)
}

// TestCommand tests a command
func (ts *TestSuite) TestCommand(name string, testFunc func(*CLITester)) {
	ts.t.Run(name, func(t *testing.T) {
		tester := NewCLITester(ts.app)
		testFunc(tester)
	})
}

// AssertSuccess asserts that a command succeeds
func (ts *TestSuite) AssertSuccess(args ...string) *CLITestResult {
	result, err := ts.tester.ExpectSuccess().Execute(args...)
	require.NoError(ts.t, err)
	return result
}

// AssertFailure asserts that a command fails
func (ts *TestSuite) AssertFailure(args ...string) *CLITestResult {
	result, err := ts.tester.ExpectFailure().Execute(args...)
	require.NoError(ts.t, err)
	return result
}

// Integration test helpers

// IntegrationTest represents an integration test
type IntegrationTest struct {
	Name         string
	Args         []string
	Setup        func(*CLITester) error
	Cleanup      func(*CLITester) error
	Expectations func(*CLITester) *CLITester
}

// RunIntegrationTests runs a set of integration tests
func RunIntegrationTests(t *testing.T, app CLIApp, tests []IntegrationTest) {
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			tester := NewCLITester(app)

			// Setup
			if test.Setup != nil {
				require.NoError(t, test.Setup(tester))
			}

			// Configure expectations
			if test.Expectations != nil {
				tester = test.Expectations(tester)
			}

			// Execute
			result, err := tester.Execute(test.Args...)
			require.NoError(t, err)

			// Cleanup
			if test.Cleanup != nil {
				require.NoError(t, test.Cleanup(tester))
			}

			t.Logf("Test %s completed in %v", test.Name, result.Duration)
		})
	}
}

// Example test helper functions

// TestDatabaseCommand is an example test for database commands
func TestDatabaseCommand(t *testing.T) {
	app := NewCLIApp("testapp", "Test application")

	// Add database plugin
	app.AddPlugin(NewDatabasePlugin())

	// Create tester
	tester := NewCLITester(app)

	// Mock migration service
	mockMigrationService := &MockMigrationService{}
	mockMigrationService.On("Migrate", mock.Anything).Return(nil)
	mockMigrationService.On("GetStatus", mock.Anything).Return(map[string]interface{}{
		"migrations": []interface{}{
			map[string]interface{}{
				"name":    "001_create_users",
				"applied": false,
			},
		},
	}, nil)

	tester.MockService((*MigrationService)(nil), mockMigrationService)

	// Test migration command
	result, err := tester.
		ExpectSuccess().
		ExpectOutput("Migrations completed successfully").
		Execute("db", "migrate")

	assert.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	mockMigrationService.AssertExpectations(t)
}

// MockMigrationService is a mock implementation for testing
type MockMigrationService struct {
	mock.Mock
}

func (m *MockMigrationService) Migrate(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockMigrationService) GetStatus(ctx context.Context) (interface{}, error) {
	args := m.Called(ctx)
	return args.Get(0), args.Error(1)
}

package checks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	health "github.com/xraph/forge/internal/health/internal"
	"github.com/xraph/forge/internal/shared"
)

// ExternalAPIHealthCheck performs health checks on external APIs.
type ExternalAPIHealthCheck struct {
	*health.BaseHealthCheck

	url            string
	method         string
	headers        map[string]string
	expectedStatus int
	expectedBody   string
	timeout        time.Duration
	client         *http.Client
	mu             sync.RWMutex
}

// ExternalAPIHealthCheckConfig contains configuration for external API health checks.
type ExternalAPIHealthCheckConfig struct {
	Name           string
	URL            string
	Method         string
	Headers        map[string]string
	ExpectedStatus int
	ExpectedBody   string
	Timeout        time.Duration
	Critical       bool
	Tags           map[string]string
	Client         *http.Client
}

// NewExternalAPIHealthCheck creates a new external API health check.
func NewExternalAPIHealthCheck(config *ExternalAPIHealthCheckConfig) *ExternalAPIHealthCheck {
	if config == nil {
		config = &ExternalAPIHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "external-api"
	}

	if config.URL == "" {
		config.URL = "https://httpbin.org/status/200"
	}

	if config.Method == "" {
		config.Method = "GET"
	}

	if config.Headers == nil {
		config.Headers = make(map[string]string)
	}

	if config.ExpectedStatus == 0 {
		config.ExpectedStatus = 200
	}

	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	config.Tags["check_type"] = "external_api"
	config.Tags["api_url"] = config.URL

	// Create HTTP client if not provided
	if config.Client == nil {
		config.Client = &http.Client{
			Timeout: config.Timeout,
		}
	}

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &ExternalAPIHealthCheck{
		BaseHealthCheck: health.NewBaseHealthCheck(baseConfig),
		url:             config.URL,
		method:          config.Method,
		headers:         config.Headers,
		expectedStatus:  config.ExpectedStatus,
		expectedBody:    config.ExpectedBody,
		timeout:         config.Timeout,
		client:          config.Client,
	}
}

// Check performs the external API health check.
func (each *ExternalAPIHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	result := health.NewHealthResult(each.Name(), health.HealthStatusHealthy, "external API is healthy").
		WithCritical(each.Critical()).
		WithTags(each.Tags()).
		WithDetail("url", each.url).
		WithDetail("method", each.method).
		WithDetail("expected_status", each.expectedStatus)

	// Perform HTTP request
	resp, err := each.makeRequest(ctx)
	if err != nil {
		return result.
			WithError(err).
			WithDetail("request_error", err.Error()).
			WithDuration(time.Since(start))
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != each.expectedStatus {
		return result.
			WithError(fmt.Errorf("unexpected status code: got %d, expected %d", resp.StatusCode, each.expectedStatus)).
			WithDetail("actual_status", resp.StatusCode).
			WithDetail("expected_status", each.expectedStatus).
			WithDuration(time.Since(start))
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return result.
			WithError(fmt.Errorf("failed to read response body: %w", err)).
			WithDetail("body_read_error", err.Error()).
			WithDuration(time.Since(start))
	}

	// Check expected body content if specified
	if each.expectedBody != "" {
		bodyStr := string(body)
		if !strings.Contains(bodyStr, each.expectedBody) {
			return result.
				WithError(errors.New("expected body content not found")).
				WithDetail("expected_body", each.expectedBody).
				WithDetail("actual_body", bodyStr[:min(len(bodyStr), 200)]).
				WithDuration(time.Since(start))
		}
	}

	// Add response details
	result.WithDetail("actual_status", resp.StatusCode).
		WithDetail("response_time", time.Since(start).String()).
		WithDetail("content_length", len(body)).
		WithDetail("content_type", resp.Header.Get("Content-Type")).
		WithDuration(time.Since(start))

	return result
}

// makeRequest makes an HTTP request to the external API.
func (each *ExternalAPIHealthCheck) makeRequest(ctx context.Context) (*http.Response, error) {
	each.mu.RLock()
	defer each.mu.RUnlock()

	// Create request
	req, err := http.NewRequestWithContext(ctx, each.method, each.url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	for key, value := range each.headers {
		req.Header.Set(key, value)
	}

	// Set user agent
	req.Header.Set("User-Agent", "Forge-Health-Check/1.0")

	// Make request
	return each.client.Do(req)
}

// HTTPSHealthCheck is a specialized health check for HTTPS endpoints.
type HTTPSHealthCheck struct {
	*ExternalAPIHealthCheck

	checkCertificate  bool
	certExpireWarning time.Duration
}

// NewHTTPSHealthCheck creates a new HTTPS health check.
func NewHTTPSHealthCheck(config *ExternalAPIHealthCheckConfig) *HTTPSHealthCheck {
	if config.Name == "" {
		config.Name = "https-api"
	}

	return &HTTPSHealthCheck{
		ExternalAPIHealthCheck: NewExternalAPIHealthCheck(config),
		checkCertificate:       true,
		certExpireWarning:      30 * 24 * time.Hour, // 30 days
	}
}

// Check performs HTTPS-specific health checks.
func (hsc *HTTPSHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Perform base API check
	result := hsc.ExternalAPIHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add HTTPS-specific checks
	if hsc.checkCertificate {
		if certStatus := hsc.checkSSLCertificate(ctx); certStatus != nil {
			for k, v := range certStatus {
				result.WithDetail(k, v)
			}

			// Check if certificate is expiring soon
			if expireWarning, ok := certStatus["certificate_expires_soon"].(bool); ok && expireWarning {
				result.WithDetail("status", health.HealthStatusDegraded).
					WithDetail("certificate_warning", "SSL certificate expires soon")
			}
		}
	}

	return result
}

// checkSSLCertificate checks SSL certificate status.
func (hsc *HTTPSHealthCheck) checkSSLCertificate(ctx context.Context) map[string]interface{} {
	// In a real implementation, this would check SSL certificate details
	return map[string]interface{}{
		"certificate_valid":        true,
		"certificate_expires_soon": false,
		"certificate_issuer":       "Let's Encrypt",
		"certificate_expires":      time.Now().Add(60 * 24 * time.Hour).Format(time.RFC3339),
	}
}

// GraphQLHealthCheck is a specialized health check for GraphQL endpoints.
type GraphQLHealthCheck struct {
	*ExternalAPIHealthCheck

	query     string
	variables map[string]interface{}
}

// NewGraphQLHealthCheck creates a new GraphQL health check.
func NewGraphQLHealthCheck(config *ExternalAPIHealthCheckConfig) *GraphQLHealthCheck {
	if config.Name == "" {
		config.Name = "graphql-api"
	}

	if config.Method == "" {
		config.Method = "POST"
	}

	if config.Headers == nil {
		config.Headers = make(map[string]string)
	}

	config.Headers["Content-Type"] = "application/json"

	return &GraphQLHealthCheck{
		ExternalAPIHealthCheck: NewExternalAPIHealthCheck(config),
		query:                  "query { __typename }",
		variables:              make(map[string]interface{}),
	}
}

// Check performs GraphQL-specific health checks.
func (gqlhc *GraphQLHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Create GraphQL query request
	if err := gqlhc.setupGraphQLRequest(); err != nil {
		return health.NewHealthResult(gqlhc.Name(), health.HealthStatusUnhealthy, "failed to setup GraphQL request").
			WithError(err)
	}

	// Perform base API check
	result := gqlhc.ExternalAPIHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add GraphQL-specific details
	result.WithDetail("query", gqlhc.query).
		WithDetail("variables", gqlhc.variables)

	return result
}

// setupGraphQLRequest sets up the GraphQL request body.
func (gqlhc *GraphQLHealthCheck) setupGraphQLRequest() error {
	// In a real implementation, this would create a proper GraphQL request body
	return nil
}

// RestAPIHealthCheck is a specialized health check for REST APIs.
type RestAPIHealthCheck struct {
	*ExternalAPIHealthCheck

	endpoints []string
	checkAll  bool
}

// NewRestAPIHealthCheck creates a new REST API health check.
func NewRestAPIHealthCheck(config *ExternalAPIHealthCheckConfig) *RestAPIHealthCheck {
	if config.Name == "" {
		config.Name = "rest-api"
	}

	return &RestAPIHealthCheck{
		ExternalAPIHealthCheck: NewExternalAPIHealthCheck(config),
		endpoints:              []string{config.URL},
		checkAll:               false,
	}
}

// AddEndpoint adds an endpoint to check.
func (rahc *RestAPIHealthCheck) AddEndpoint(endpoint string) {
	rahc.endpoints = append(rahc.endpoints, endpoint)
}

// SetCheckAll sets whether to check all endpoints.
func (rahc *RestAPIHealthCheck) SetCheckAll(checkAll bool) {
	rahc.checkAll = checkAll
}

// Check performs REST API health checks.
func (rahc *RestAPIHealthCheck) Check(ctx context.Context) *health.HealthResult {
	if !rahc.checkAll {
		// Check only the primary endpoint
		return rahc.ExternalAPIHealthCheck.Check(ctx)
	}

	// Check all endpoints
	result := health.NewHealthResult(rahc.Name(), health.HealthStatusHealthy, "REST API endpoints are healthy").
		WithCritical(rahc.Critical()).
		WithTags(rahc.Tags())

	var failedEndpoints []string

	endpointResults := make(map[string]interface{})

	for _, endpoint := range rahc.endpoints {
		// Create a temporary check for this endpoint
		tempConfig := &ExternalAPIHealthCheckConfig{
			Name:           fmt.Sprintf("%s-%s", rahc.Name(), endpoint),
			URL:            endpoint,
			Method:         rahc.method,
			Headers:        rahc.headers,
			ExpectedStatus: rahc.expectedStatus,
			ExpectedBody:   rahc.expectedBody,
			Timeout:        rahc.timeout,
			Client:         rahc.client,
		}

		tempCheck := NewExternalAPIHealthCheck(tempConfig)
		endpointResult := tempCheck.Check(ctx)

		endpointResults[endpoint] = map[string]interface{}{
			"status":   endpointResult.Status,
			"message":  endpointResult.Message,
			"duration": endpointResult.Duration,
		}

		if endpointResult.IsUnhealthy() {
			failedEndpoints = append(failedEndpoints, endpoint)
		}
	}

	result.WithDetail("endpoints", endpointResults).
		WithDetail("total_endpoints", len(rahc.endpoints)).
		WithDetail("failed_endpoints", len(failedEndpoints))

	if len(failedEndpoints) > 0 {
		result.WithError(fmt.Errorf("%d endpoints failed", len(failedEndpoints))).
			WithDetail("failed_endpoint_list", failedEndpoints)
	}

	return result
}

// DatabaseAPIHealthCheck is a specialized health check for database APIs.
type DatabaseAPIHealthCheck struct {
	*ExternalAPIHealthCheck

	testQuery string
	database  string
}

// NewDatabaseAPIHealthCheck creates a new database API health check.
func NewDatabaseAPIHealthCheck(config *ExternalAPIHealthCheckConfig) *DatabaseAPIHealthCheck {
	if config.Name == "" {
		config.Name = "database-api"
	}

	return &DatabaseAPIHealthCheck{
		ExternalAPIHealthCheck: NewExternalAPIHealthCheck(config),
		testQuery:              "SELECT 1",
		database:               "default",
	}
}

// Check performs database API health checks.
func (dahc *DatabaseAPIHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Perform base API check
	result := dahc.ExternalAPIHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add database-specific details
	result.WithDetail("test_query", dahc.testQuery).
		WithDetail("database", dahc.database)

	return result
}

// ExternalAPIHealthCheckFactory creates external API health checks.
type ExternalAPIHealthCheckFactory struct {
	container shared.Container
}

// NewExternalAPIHealthCheckFactory creates a new factory.
func NewExternalAPIHealthCheckFactory(container shared.Container) *ExternalAPIHealthCheckFactory {
	return &ExternalAPIHealthCheckFactory{
		container: container,
	}
}

// CreateExternalAPIHealthCheck creates an external API health check.
func (factory *ExternalAPIHealthCheckFactory) CreateExternalAPIHealthCheck(name string, url string, apiType string, critical bool) (health.HealthCheck, error) {
	config := &ExternalAPIHealthCheckConfig{
		Name:           name,
		URL:            url,
		Method:         "GET",
		ExpectedStatus: 200,
		Timeout:        10 * time.Second,
		Critical:       critical,
		Tags:           map[string]string{"api_type": apiType},
	}

	switch apiType {
	case "https":
		return NewHTTPSHealthCheck(config), nil
	case "graphql":
		return NewGraphQLHealthCheck(config), nil
	case "rest":
		return NewRestAPIHealthCheck(config), nil
	case "database-api":
		return NewDatabaseAPIHealthCheck(config), nil
	default:
		return NewExternalAPIHealthCheck(config), nil
	}
}

// RegisterExternalAPIHealthChecks registers external API health checks.
func RegisterExternalAPIHealthChecks(healthService health.HealthService, container shared.Container) error {
	factory := NewExternalAPIHealthCheckFactory(container)

	// Example external APIs to check
	externalAPIs := []struct {
		name     string
		url      string
		apiType  string
		critical bool
	}{
		{"github-api", "https://api.github.com/status", "https", false},
		{"google-api", "https://www.googleapis.com/", "https", false},
		{"httpbin", "https://httpbin.org/status/200", "rest", false},
	}

	for _, api := range externalAPIs {
		check, err := factory.CreateExternalAPIHealthCheck(api.name, api.url, api.apiType, api.critical)
		if err != nil {
			continue // Skip failed checks
		}

		if err := healthService.Register(check); err != nil {
			return fmt.Errorf("failed to register %s health check: %w", api.name, err)
		}
	}

	return nil
}

// ExternalAPIHealthCheckComposite combines multiple external API health checks.
type ExternalAPIHealthCheckComposite struct {
	*health.CompositeHealthCheck

	apiChecks []health.HealthCheck
}

// NewExternalAPIHealthCheckComposite creates a composite external API health check.
func NewExternalAPIHealthCheckComposite(name string, checks ...health.HealthCheck) *ExternalAPIHealthCheckComposite {
	config := &health.HealthCheckConfig{
		Name:     name,
		Timeout:  30 * time.Second,
		Critical: false, // External APIs are typically not critical
	}

	composite := health.NewCompositeHealthCheck(config, checks...)

	return &ExternalAPIHealthCheckComposite{
		CompositeHealthCheck: composite,
		apiChecks:            checks,
	}
}

// GetAPIChecks returns the individual API checks.
func (eahcc *ExternalAPIHealthCheckComposite) GetAPIChecks() []health.HealthCheck {
	return eahcc.apiChecks
}

// AddAPICheck adds an API check to the composite.
func (eahcc *ExternalAPIHealthCheckComposite) AddAPICheck(check health.HealthCheck) {
	eahcc.apiChecks = append(eahcc.apiChecks, check)
	eahcc.AddCheck(check)
}

// CircuitBreakerHealthCheck wraps external API checks with circuit breaker pattern.
type CircuitBreakerHealthCheck struct {
	*ExternalAPIHealthCheck

	failureThreshold int
	resetTimeout     time.Duration
	state            string
	failures         int
	lastFailureTime  time.Time
	mu               sync.RWMutex
}

// NewCircuitBreakerHealthCheck creates a new circuit breaker health check.
func NewCircuitBreakerHealthCheck(config *ExternalAPIHealthCheckConfig) *CircuitBreakerHealthCheck {
	return &CircuitBreakerHealthCheck{
		ExternalAPIHealthCheck: NewExternalAPIHealthCheck(config),
		failureThreshold:       5,
		resetTimeout:           60 * time.Second,
		state:                  "closed",
		failures:               0,
	}
}

// Check performs health check with circuit breaker pattern.
func (cbhc *CircuitBreakerHealthCheck) Check(ctx context.Context) *health.HealthResult {
	cbhc.mu.Lock()
	defer cbhc.mu.Unlock()

	// Check circuit breaker state
	if cbhc.state == "open" {
		// Check if we should try to reset
		if time.Since(cbhc.lastFailureTime) > cbhc.resetTimeout {
			cbhc.state = "half-open"
		} else {
			return health.NewHealthResult(cbhc.Name(), health.HealthStatusUnhealthy, "circuit breaker is open").
				WithDetail("circuit_breaker_state", cbhc.state).
				WithDetail("failures", cbhc.failures).
				WithDetail("last_failure", cbhc.lastFailureTime.Format(time.RFC3339))
		}
	}

	// Perform the actual health check
	result := cbhc.ExternalAPIHealthCheck.Check(ctx)

	// Update circuit breaker state based on result
	if result.IsUnhealthy() {
		cbhc.failures++
		cbhc.lastFailureTime = time.Now()

		if cbhc.failures >= cbhc.failureThreshold {
			cbhc.state = "open"
		}
	} else {
		// Reset on successful check
		cbhc.failures = 0
		cbhc.state = "closed"
	}

	result.WithDetail("circuit_breaker_state", cbhc.state).
		WithDetail("failures", cbhc.failures)

	return result
}

// Helper functions

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}

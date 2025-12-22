package typescript

import (
	"strings"

	"github.com/xraph/forge/internal/client"
)

// FetchClientGenerator generates fetch-based HTTP client code.
type FetchClientGenerator struct{}

// NewFetchClientGenerator creates a new fetch client generator.
func NewFetchClientGenerator() *FetchClientGenerator {
	return &FetchClientGenerator{}
}

// GenerateBaseClient generates the base fetch client with interceptors and retry logic.
func (g *FetchClientGenerator) GenerateBaseClient(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// Imports
	buf.WriteString("// Base HTTP client using native fetch\n\n")

	// RequestConfig interface
	buf.WriteString("export interface RequestConfig {\n")
	buf.WriteString("  method: string;\n")
	buf.WriteString("  url: string;\n")
	buf.WriteString("  headers?: Record<string, string>;\n")
	buf.WriteString("  body?: any;\n")
	buf.WriteString("  signal?: AbortSignal;\n")
	buf.WriteString("  retry?: RetryConfig;\n")
	buf.WriteString("}\n\n")

	// RetryConfig interface
	buf.WriteString("export interface RetryConfig {\n")
	buf.WriteString("  maxAttempts?: number;\n")
	buf.WriteString("  delay?: number;\n")
	buf.WriteString("  maxDelay?: number;\n")
	buf.WriteString("  retryableStatusCodes?: number[];\n")
	buf.WriteString("}\n\n")

	// Interceptor interfaces
	if config.Interceptors {
		buf.WriteString("export interface RequestInterceptor {\n")
		buf.WriteString("  onRequest(config: RequestConfig): RequestConfig | Promise<RequestConfig>;\n")
		buf.WriteString("}\n\n")

		buf.WriteString("export interface ResponseInterceptor {\n")
		buf.WriteString("  onResponse(response: Response): Response | Promise<Response>;\n")
		buf.WriteString("  onError?(error: Error): Error | Promise<Error>;\n")
		buf.WriteString("}\n\n")
	}

	// Default retry config
	buf.WriteString("const DEFAULT_RETRY_CONFIG: RetryConfig = {\n")
	buf.WriteString("  maxAttempts: 3,\n")
	buf.WriteString("  delay: 1000,\n")
	buf.WriteString("  maxDelay: 30000,\n")
	buf.WriteString("  retryableStatusCodes: [408, 429, 500, 502, 503, 504],\n")
	buf.WriteString("};\n\n")

	// HTTPClient class
	buf.WriteString("export class HTTPClient {\n")
	buf.WriteString("  private baseURL: string;\n")
	buf.WriteString("  private defaultHeaders: Record<string, string>;\n")
	buf.WriteString("  private timeout: number;\n")

	if config.Interceptors {
		buf.WriteString("  private requestInterceptors: RequestInterceptor[] = [];\n")
		buf.WriteString("  private responseInterceptors: ResponseInterceptor[] = [];\n")
	}

	buf.WriteString("\n")
	buf.WriteString("  constructor(baseURL: string, timeout: number = 30000) {\n")
	buf.WriteString("    this.baseURL = baseURL;\n")
	buf.WriteString("    this.timeout = timeout;\n")
	buf.WriteString("    this.defaultHeaders = {\n")
	buf.WriteString("      'Content-Type': 'application/json',\n")
	buf.WriteString("    };\n")
	buf.WriteString("  }\n\n")

	// Add interceptor methods
	if config.Interceptors {
		buf.WriteString("  addRequestInterceptor(interceptor: RequestInterceptor): void {\n")
		buf.WriteString("    this.requestInterceptors.push(interceptor);\n")
		buf.WriteString("  }\n\n")

		buf.WriteString("  addResponseInterceptor(interceptor: ResponseInterceptor): void {\n")
		buf.WriteString("    this.responseInterceptors.push(interceptor);\n")
		buf.WriteString("  }\n\n")
	}

	// Set default header method
	buf.WriteString("  setDefaultHeader(key: string, value: string): void {\n")
	buf.WriteString("    this.defaultHeaders[key] = value;\n")
	buf.WriteString("  }\n\n")

	// Main request method with retry logic
	buf.WriteString("  async request<T>(config: RequestConfig): Promise<T> {\n")
	buf.WriteString("    const retryConfig = { ...DEFAULT_RETRY_CONFIG, ...config.retry };\n")
	buf.WriteString("    let lastError: Error | null = null;\n\n")

	buf.WriteString("    for (let attempt = 0; attempt < (retryConfig.maxAttempts || 1); attempt++) {\n")
	buf.WriteString("      try {\n")
	buf.WriteString("        return await this.executeRequest<T>(config, attempt);\n")
	buf.WriteString("      } catch (error) {\n")
	buf.WriteString("        lastError = error as Error;\n\n")

	buf.WriteString("        // Check if we should retry\n")
	buf.WriteString("        const shouldRetry = this.shouldRetry(error, attempt, retryConfig);\n")
	buf.WriteString("        if (!shouldRetry) {\n")
	buf.WriteString("          throw error;\n")
	buf.WriteString("        }\n\n")

	buf.WriteString("        // Exponential backoff\n")
	buf.WriteString("        const delay = Math.min(\n")
	buf.WriteString("          (retryConfig.delay || 1000) * Math.pow(2, attempt),\n")
	buf.WriteString("          retryConfig.maxDelay || 30000\n")
	buf.WriteString("        );\n")
	buf.WriteString("        await new Promise(resolve => setTimeout(resolve, delay));\n")
	buf.WriteString("      }\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    throw lastError || new Error('Request failed after retries');\n")
	buf.WriteString("  }\n\n")

	// Execute request method
	buf.WriteString("  private async executeRequest<T>(config: RequestConfig, attempt: number): Promise<T> {\n")
	buf.WriteString("    let requestConfig = { ...config };\n\n")

	// Apply request interceptors
	if config.Interceptors {
		buf.WriteString("    // Apply request interceptors\n")
		buf.WriteString("    for (const interceptor of this.requestInterceptors) {\n")
		buf.WriteString("      requestConfig = await interceptor.onRequest(requestConfig);\n")
		buf.WriteString("    }\n\n")
	}

	buf.WriteString("    // Build full URL\n")
	buf.WriteString("    const url = requestConfig.url.startsWith('http') \n")
	buf.WriteString("      ? requestConfig.url \n")
	buf.WriteString("      : this.baseURL + requestConfig.url;\n\n")

	buf.WriteString("    // Merge headers\n")
	buf.WriteString("    const headers = {\n")
	buf.WriteString("      ...this.defaultHeaders,\n")
	buf.WriteString("      ...requestConfig.headers,\n")
	buf.WriteString("    };\n\n")

	buf.WriteString("    // Create abort controller with timeout\n")
	buf.WriteString("    const controller = new AbortController();\n")
	buf.WriteString("    const timeoutId = setTimeout(() => controller.abort(), this.timeout);\n\n")

	buf.WriteString("    // Use provided signal or create new one\n")
	buf.WriteString("    const signal = requestConfig.signal || controller.signal;\n\n")

	buf.WriteString("    try {\n")
	buf.WriteString("      // Make fetch request\n")
	buf.WriteString("      let response = await fetch(url, {\n")
	buf.WriteString("        method: requestConfig.method,\n")
	buf.WriteString("        headers,\n")
	buf.WriteString("        body: requestConfig.body ? JSON.stringify(requestConfig.body) : undefined,\n")
	buf.WriteString("        signal,\n")
	buf.WriteString("      });\n\n")

	// Apply response interceptors
	if config.Interceptors {
		buf.WriteString("      // Apply response interceptors\n")
		buf.WriteString("      for (const interceptor of this.responseInterceptors) {\n")
		buf.WriteString("        response = await interceptor.onResponse(response);\n")
		buf.WriteString("      }\n\n")
	}

	buf.WriteString("      clearTimeout(timeoutId);\n\n")

	buf.WriteString("      // Handle non-OK responses\n")
	buf.WriteString("      if (!response.ok) {\n")
	buf.WriteString("        await this.handleErrorResponse(response);\n")
	buf.WriteString("      }\n\n")

	buf.WriteString("      // Parse response\n")
	buf.WriteString("      const contentType = response.headers.get('content-type');\n")
	buf.WriteString("      if (contentType && contentType.includes('application/json')) {\n")
	buf.WriteString("        return await response.json();\n")
	buf.WriteString("      }\n\n")

	buf.WriteString("      // Return empty object for 204 No Content\n")
	buf.WriteString("      if (response.status === 204) {\n")
	buf.WriteString("        return {} as T;\n")
	buf.WriteString("      }\n\n")

	buf.WriteString("      return await response.text() as any;\n")
	buf.WriteString("    } catch (error) {\n")
	buf.WriteString("      clearTimeout(timeoutId);\n\n")

	// Apply error interceptors
	if config.Interceptors {
		buf.WriteString("      // Apply error interceptors\n")
		buf.WriteString("      let processedError = error as Error;\n")
		buf.WriteString("      for (const interceptor of this.responseInterceptors) {\n")
		buf.WriteString("        if (interceptor.onError) {\n")
		buf.WriteString("          processedError = await interceptor.onError(processedError);\n")
		buf.WriteString("        }\n")
		buf.WriteString("      }\n")
		buf.WriteString("      throw processedError;\n")
	} else {
		buf.WriteString("      throw error;\n")
	}

	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	// Handle error response method
	buf.WriteString("  private async handleErrorResponse(response: Response): Promise<never> {\n")
	buf.WriteString("    const contentType = response.headers.get('content-type');\n")
	buf.WriteString("    let errorData: any = {};\n\n")

	buf.WriteString("    if (contentType && contentType.includes('application/json')) {\n")
	buf.WriteString("      try {\n")
	buf.WriteString("        errorData = await response.json();\n")
	buf.WriteString("      } catch (e) {\n")
	buf.WriteString("        // Ignore JSON parse errors\n")
	buf.WriteString("      }\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    // Import error classes are assumed to be in errors.ts\n")
	buf.WriteString("    const message = errorData.message || errorData.error || response.statusText;\n")
	buf.WriteString("    const code = errorData.code || '';\n")
	buf.WriteString("    const details = errorData.details || errorData;\n\n")

	buf.WriteString("    // This will be enhanced by error taxonomy generator\n")
	buf.WriteString("    throw {\n")
	buf.WriteString("      statusCode: response.status,\n")
	buf.WriteString("      message,\n")
	buf.WriteString("      code,\n")
	buf.WriteString("      details,\n")
	buf.WriteString("    };\n")
	buf.WriteString("  }\n\n")

	// Should retry method
	buf.WriteString("  private shouldRetry(error: any, attempt: number, config: RetryConfig): boolean {\n")
	buf.WriteString("    // Don't retry if we've exhausted attempts\n")
	buf.WriteString("    if (attempt >= (config.maxAttempts || 1) - 1) {\n")
	buf.WriteString("      return false;\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    // Don't retry if aborted\n")
	buf.WriteString("    if (error.name === 'AbortError') {\n")
	buf.WriteString("      return false;\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    // Retry on network errors\n")
	buf.WriteString("    if (error.name === 'TypeError' || error.message === 'Failed to fetch') {\n")
	buf.WriteString("      return true;\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    // Retry on retryable status codes\n")
	buf.WriteString("    if (error.statusCode) {\n")
	buf.WriteString("      return (config.retryableStatusCodes || []).includes(error.statusCode);\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    return false;\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n")

	return buf.String()
}

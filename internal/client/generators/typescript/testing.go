package typescript

import (
	"strings"

	"github.com/xraph/forge/internal/client"
)

// TestingGenerator generates Jest test configuration and scaffolding.
type TestingGenerator struct{}

// NewTestingGenerator creates a new testing generator.
func NewTestingGenerator() *TestingGenerator {
	return &TestingGenerator{}
}

// GenerateJestConfig generates jest.config.js.
func (t *TestingGenerator) GenerateJestConfig(spec *client.APISpec, config client.GeneratorConfig) string {
	return `/** @type {import('jest').Config} */
module.exports = {
  preset: 'ts-jest',
  testEnvironment: 'node',
  roots: ['<rootDir>/src', '<rootDir>/tests'],
  testMatch: ['**/__tests__/**/*.ts', '**/?(*.)+(spec|test).ts'],
  transform: {
    '^.+\\.ts$': 'ts-jest',
  },
  collectCoverageFrom: [
    'src/**/*.ts',
    '!src/**/*.d.ts',
    '!src/**/__tests__/**',
  ],
  coverageThreshold: {
    global: {
      branches: 80,
      functions: 80,
      lines: 80,
      statements: 80,
    },
  },
  moduleNameMapper: {
    '^@/(.*)$': '<rootDir>/src/$1',
  },
  verbose: true,
};
`
}

// GenerateExampleTest generates an example test file.
func (t *TestingGenerator) GenerateExampleTest(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("import { " + config.APIName + " } from '../src/client';\n\n")

	buf.WriteString("describe('" + config.APIName + "', () => {\n")
	buf.WriteString("  it('should create a client instance', () => {\n")
	buf.WriteString("    const client = new " + config.APIName + "({\n")
	buf.WriteString("      baseURL: 'https://api.example.com',\n")
	buf.WriteString("    });\n\n")
	buf.WriteString("    expect(client).toBeDefined();\n")
	buf.WriteString("  });\n\n")

	buf.WriteString("  it('should set auth headers', () => {\n")
	buf.WriteString("    const client = new " + config.APIName + "({\n")
	buf.WriteString("      baseURL: 'https://api.example.com',\n")
	buf.WriteString("      auth: {\n")
	buf.WriteString("        bearerToken: 'test-token',\n")
	buf.WriteString("      },\n")
	buf.WriteString("    });\n\n")
	buf.WriteString("    expect(client).toBeDefined();\n")
	buf.WriteString("  });\n")
	buf.WriteString("});\n")

	return buf.String()
}

// GenerateTestUtils generates test utility functions.
func (t *TestingGenerator) GenerateTestUtils(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("// Test utilities and mocks\n\n")

	buf.WriteString("/**\n")
	buf.WriteString(" * Mock fetch for testing\n")
	buf.WriteString(" */\n")
	buf.WriteString("export function mockFetch(response: any, status: number = 200): void {\n")
	buf.WriteString("  global.fetch = jest.fn(() =>\n")
	buf.WriteString("    Promise.resolve({\n")
	buf.WriteString("      ok: status >= 200 && status < 300,\n")
	buf.WriteString("      status,\n")
	buf.WriteString("      statusText: 'OK',\n")
	buf.WriteString("      headers: new Headers({\n")
	buf.WriteString("        'content-type': 'application/json',\n")
	buf.WriteString("      }),\n")
	buf.WriteString("      json: async () => response,\n")
	buf.WriteString("      text: async () => JSON.stringify(response),\n")
	buf.WriteString("    } as Response)\n")
	buf.WriteString("  );\n")
	buf.WriteString("}\n\n")

	buf.WriteString("/**\n")
	buf.WriteString(" * Reset fetch mock\n")
	buf.WriteString(" */\n")
	buf.WriteString("export function resetFetchMock(): void {\n")
	buf.WriteString("  if (jest.isMockFunction(global.fetch)) {\n")
	buf.WriteString("    (global.fetch as jest.Mock).mockReset();\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n\n")

	buf.WriteString("/**\n")
	buf.WriteString(" * Create a mock API client for testing\n")
	buf.WriteString(" */\n")
	buf.WriteString("export function createMockClient(baseURL: string = 'https://api.test.com') {\n")
	buf.WriteString("  return {\n")
	buf.WriteString("    baseURL,\n")
	buf.WriteString("    auth: {\n")
	buf.WriteString("      bearerToken: 'test-token',\n")
	buf.WriteString("    },\n")
	buf.WriteString("  };\n")
	buf.WriteString("}\n")

	return buf.String()
}


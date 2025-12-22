package typescript

import (
	"github.com/xraph/forge/internal/client"
)

// LintingGenerator generates ESLint and Prettier configuration.
type LintingGenerator struct{}

// NewLintingGenerator creates a new linting generator.
func NewLintingGenerator() *LintingGenerator {
	return &LintingGenerator{}
}

// GenerateESLintConfig generates .eslintrc.js.
func (l *LintingGenerator) GenerateESLintConfig(spec *client.APISpec, config client.GeneratorConfig) string {
	return `module.exports = {
  parser: '@typescript-eslint/parser',
  parserOptions: {
    ecmaVersion: 2020,
    sourceType: 'module',
    project: './tsconfig.json',
  },
  extends: [
    'eslint:recommended',
    'plugin:@typescript-eslint/recommended',
    'plugin:@typescript-eslint/recommended-requiring-type-checking',
  ],
  plugins: ['@typescript-eslint'],
  env: {
    node: true,
    es2020: true,
  },
  rules: {
    '@typescript-eslint/explicit-module-boundary-types': 'off',
    '@typescript-eslint/no-explicit-any': 'warn',
    '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
    '@typescript-eslint/no-floating-promises': 'error',
    'no-console': ['warn', { allow: ['warn', 'error'] }],
  },
  ignorePatterns: ['dist', 'node_modules', '*.js'],
};
`
}

// GeneratePrettierConfig generates .prettierrc.
func (l *LintingGenerator) GeneratePrettierConfig(spec *client.APISpec, config client.GeneratorConfig) string {
	return `{
  "semi": true,
  "trailingComma": "es5",
  "singleQuote": true,
  "printWidth": 100,
  "tabWidth": 2,
  "useTabs": false,
  "arrowParens": "always",
  "endOfLine": "lf"
}
`
}

// GeneratePrettierIgnore generates .prettierignore.
func (l *LintingGenerator) GeneratePrettierIgnore(spec *client.APISpec, config client.GeneratorConfig) string {
	return `# Dependencies
node_modules

# Build output
dist
coverage

# Package files
package-lock.json
yarn.lock
pnpm-lock.yaml
`
}

// GenerateESLintIgnore generates .eslintignore.
func (l *LintingGenerator) GenerateESLintIgnore(spec *client.APISpec, config client.GeneratorConfig) string {
	return `# Dependencies
node_modules

# Build output
dist
coverage

# Config files
*.config.js
jest.config.js
`
}

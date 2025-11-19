package typescript

import (
	"github.com/xraph/forge/internal/client"
)

// NPMIgnoreGenerator generates .npmignore file.
type NPMIgnoreGenerator struct{}

// NewNPMIgnoreGenerator creates a new npmignore generator.
func NewNPMIgnoreGenerator() *NPMIgnoreGenerator {
	return &NPMIgnoreGenerator{}
}

// Generate generates .npmignore content.
func (n *NPMIgnoreGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	return `# Source files
src
tests
__tests__

# Configuration files
tsconfig.json
jest.config.js
.eslintrc.js
.prettierrc
.prettierignore
.eslintignore

# CI/CD
.github
.gitlab-ci.yml
.travis.yml

# Development files
*.test.ts
*.spec.ts
coverage

# Editor
.vscode
.idea
*.swp
*.swo

# Logs
*.log

# Environment
.env
.env.*

# Build artifacts (keep only dist/)
node_modules
.DS_Store
*.tsbuildinfo

# Documentation (source)
docs

# Git
.git
.gitignore
.gitattributes
`
}


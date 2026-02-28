package plugins

// contributorYAMLTemplate is the template for forge.contributor.yaml.
const contributorYAMLTemplate = `name: {{.Name}}
display_name: {{.DisplayName}}
version: 1.0.0
type: {{.Framework}}
build:
  mode: {{.Mode}}
{{- if ne .Framework "templ" }}
  ui_dir: ui
{{- end }}
{{- if .DistDir }}
  dist_dir: {{.DistDir}}
{{- end }}

nav:
  - label: Overview
    path: /
    icon: layout
    group: Platform
    priority: 0

widgets:
  - id: {{.Name}}-status
    title: "{{.DisplayName}} Status"
    size: sm
    refresh_sec: 30

settings:
  - id: {{.Name}}-config
    title: "{{.DisplayName}} Settings"
    description: "Configure {{.DisplayName}}"

searchable: true
`

// astroPackageJSONTemplate is the package.json template for Astro contributors.
const astroPackageJSONTemplate = `{
  "name": "@forge-ext/{{.Name}}-dashboard",
  "version": "1.0.0",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "astro dev",
    "build": "astro build",
    "preview": "astro preview"
  },
  "dependencies": {
    "astro": "^5.0.0"
  }
}
`

// astroConfigTemplate is the astro.config.mjs template.
const astroConfigTemplate = `import { defineConfig } from 'astro/config';

export default defineConfig({
  output: '{{.AstroOutput}}',
  build: {
    format: 'directory',
  },
  // Asset paths will be rewritten by the Forge dashboard shell
  vite: {
    build: {
      assetsDir: 'assets',
    },
  },
});
`

// astroPageTemplate is a sample .astro page template.
const astroPageTemplate = `---
// {{.DisplayName}} - Overview Page
---
<div class="space-y-6">
  <div class="grid grid-cols-3 gap-4">
    <div class="rounded-lg border p-4">
      <p class="text-sm text-muted-foreground">Status</p>
      <p class="text-2xl font-bold">Active</p>
    </div>
    <div class="rounded-lg border p-4">
      <p class="text-sm text-muted-foreground">Items</p>
      <p class="text-2xl font-bold">0</p>
    </div>
    <div class="rounded-lg border p-4">
      <p class="text-sm text-muted-foreground">Health</p>
      <p class="text-2xl font-bold text-green-500">OK</p>
    </div>
  </div>

  <div class="rounded-lg border p-6">
    <h2 class="text-lg font-semibold mb-4">{{.DisplayName}} Overview</h2>
    <p class="text-muted-foreground">
      This is the {{.DisplayName}} dashboard contributor. Edit
      <code>ui/src/pages/index.astro</code> to customize.
    </p>
  </div>
</div>
`

// astroWidgetTemplate is a sample .astro widget fragment template.
const astroWidgetTemplate = `---
// Widget: {{.DisplayName}} Status
---
<div class="flex items-center justify-between">
  <span class="text-3xl font-bold">OK</span>
  <span class="text-xs text-muted-foreground">{{.DisplayName}}</span>
</div>
`

// nextjsPackageJSONTemplate is the package.json template for Next.js contributors.
const nextjsPackageJSONTemplate = `{
  "name": "@forge-ext/{{.Name}}-dashboard",
  "version": "1.0.0",
  "private": true,
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start"
  },
  "dependencies": {
    "next": "^15.0.0",
    "react": "^19.0.0",
    "react-dom": "^19.0.0"
  },
  "devDependencies": {
    "@types/react": "^19.0.0",
    "typescript": "^5.0.0"
  }
}
`

// nextjsConfigTemplate is the next.config.mjs template.
const nextjsConfigTemplate = `/** @type {import('next').NextConfig} */
const nextConfig = {
{{- if eq .Mode "static" }}
  output: 'export',
  distDir: 'out',
{{- else }}
  output: 'standalone',
{{- end }}
  // Disable image optimization for static export
  images: {
    unoptimized: true,
  },
};

export default nextConfig;
`

// nextjsPageTemplate is a sample page.tsx template for Next.js contributors.
const nextjsPageTemplate = `export default function Page() {
  return (
    <div className="space-y-6">
      <div className="grid grid-cols-3 gap-4">
        <div className="rounded-lg border p-4">
          <p className="text-sm text-muted-foreground">Status</p>
          <p className="text-2xl font-bold">Active</p>
        </div>
        <div className="rounded-lg border p-4">
          <p className="text-sm text-muted-foreground">Items</p>
          <p className="text-2xl font-bold">0</p>
        </div>
        <div className="rounded-lg border p-4">
          <p className="text-sm text-muted-foreground">Health</p>
          <p className="text-2xl font-bold text-green-500">OK</p>
        </div>
      </div>

      <div className="rounded-lg border p-6">
        <h2 className="text-lg font-semibold mb-4">{{.DisplayName}} Overview</h2>
        <p className="text-muted-foreground">
          This is the {{.DisplayName}} dashboard contributor. Edit{" "}
          <code>ui/app/page.tsx</code> to customize.
        </p>
      </div>
    </div>
  );
}
`

// nextjsLayoutTemplate is a root layout.tsx template for Next.js.
const nextjsLayoutTemplate = `export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return children;
}
`

// nextjsTSConfigTemplate is the tsconfig.json template for Next.js contributors.
const nextjsTSConfigTemplate = `{
  "compilerOptions": {
    "target": "ES2017",
    "lib": ["dom", "dom.iterable", "esnext"],
    "allowJs": true,
    "skipLibCheck": true,
    "strict": true,
    "noEmit": true,
    "esModuleInterop": true,
    "module": "esnext",
    "moduleResolution": "bundler",
    "resolveJsonModule": true,
    "isolatedModules": true,
    "jsx": "preserve",
    "incremental": true,
    "plugins": [{ "name": "next" }],
    "paths": { "@/*": ["./*"] }
  },
  "include": ["next-env.d.ts", "**/*.ts", "**/*.tsx"],
  "exclude": ["node_modules"]
}
`

// templContributorGoTemplate is the main contributor.go template for templ contributors.
const templContributorGoTemplate = `package {{.Name}}

import (
	"context"

	"github.com/a-h/templ"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// Contributor implements the dashboard LocalContributor interface.
type Contributor struct {
	manifest *contributor.Manifest
}

// New creates a new {{.DisplayName}} contributor.
func New(manifest *contributor.Manifest) *Contributor {
	return &Contributor{manifest: manifest}
}

// Manifest returns the contributor manifest.
func (c *Contributor) Manifest() *contributor.Manifest {
	return c.manifest
}

// RenderPage renders a page for the given route.
func (c *Contributor) RenderPage(ctx context.Context, route string, params contributor.Params) (templ.Component, error) {
	switch route {
	case "/":
		return OverviewPage(), nil
	default:
		return nil, contributor.ErrPageNotFound
	}
}

// RenderWidget renders a widget by ID.
func (c *Contributor) RenderWidget(ctx context.Context, widgetID string) (templ.Component, error) {
	switch widgetID {
	case "{{.Name}}-status":
		return StatusWidget(), nil
	default:
		return nil, contributor.ErrWidgetNotFound
	}
}

// RenderSettings renders a settings panel by ID.
func (c *Contributor) RenderSettings(ctx context.Context, settingID string) (templ.Component, error) {
	switch settingID {
	case "{{.Name}}-config":
		return SettingsPanel(), nil
	default:
		return nil, contributor.ErrSettingsNotFound
	}
}
`

// templPageTemplate is a sample overview.templ page template.
const templPageTemplate = `package {{.Name}}

import "github.com/xraph/forgeui/icons"

// OverviewPage renders the main overview page for {{.DisplayName}}.
templ OverviewPage() {
	<div class="space-y-6">
		<div class="grid grid-cols-1 gap-4 md:grid-cols-3">
			<div class="rounded-lg border bg-card p-4 text-card-foreground shadow-sm">
				<div class="flex items-center gap-2">
					@icons.Activity(icons.WithSize(16), icons.WithClass("text-primary"))
					<p class="text-sm text-muted-foreground">Status</p>
				</div>
				<p class="mt-2 text-2xl font-bold">Active</p>
			</div>
			<div class="rounded-lg border bg-card p-4 text-card-foreground shadow-sm">
				<div class="flex items-center gap-2">
					@icons.Hash(icons.WithSize(16), icons.WithClass("text-primary"))
					<p class="text-sm text-muted-foreground">Items</p>
				</div>
				<p class="mt-2 text-2xl font-bold">0</p>
			</div>
			<div class="rounded-lg border bg-card p-4 text-card-foreground shadow-sm">
				<div class="flex items-center gap-2">
					@icons.HeartPulse(icons.WithSize(16), icons.WithClass("text-green-500"))
					<p class="text-sm text-muted-foreground">Health</p>
				</div>
				<p class="mt-2 text-2xl font-bold text-green-500">OK</p>
			</div>
		</div>
		<div class="rounded-lg border bg-card p-6 text-card-foreground shadow-sm">
			<h2 class="text-lg font-semibold mb-4">{{.DisplayName}} Overview</h2>
			<p class="text-muted-foreground">
				This is the {{.DisplayName}} dashboard contributor.
				Edit <code class="text-sm bg-muted px-1 py-0.5 rounded">pages.templ</code> to customize.
			</p>
		</div>
	</div>
}
`

// templWidgetTemplate is a sample status widget templ template.
const templWidgetTemplate = `package {{.Name}}

import "github.com/xraph/forgeui/icons"

// StatusWidget renders the {{.DisplayName}} status widget.
templ StatusWidget() {
	<div class="flex items-center justify-between">
		<div class="flex items-center gap-2">
			@icons.Activity(icons.WithSize(16), icons.WithClass("text-green-500"))
			<span class="text-3xl font-bold">OK</span>
		</div>
		<span class="text-xs text-muted-foreground">{{.DisplayName}}</span>
	</div>
}
`

// templSettingsTemplate is a sample settings panel templ template.
const templSettingsTemplate = `package {{.Name}}

// SettingsPanel renders the {{.DisplayName}} settings panel.
templ SettingsPanel() {
	<div class="space-y-4">
		<div>
			<h3 class="text-lg font-medium">{{.DisplayName}} Settings</h3>
			<p class="text-sm text-muted-foreground">Configure {{.DisplayName}} behavior.</p>
		</div>
		<div class="rounded-lg border p-4">
			<p class="text-sm text-muted-foreground">No settings available yet.</p>
		</div>
	</div>
}
`

// scaffoldTemplateData holds data for scaffold templates.
type scaffoldTemplateData struct {
	Name        string
	DisplayName string
	Framework   string
	Mode        string
	DistDir     string
	AstroOutput string // "static" or "server"
}

package server

import (
	"embed"
	"html/template"
	"net/http"

	"github.com/xraph/forge"
)

//go:embed assets/*
var dashboardAssets embed.FS

// Dashboard provides a web UI for SDK monitoring and management
type Dashboard struct {
	server  *Server
	logger  forge.Logger
	metrics forge.Metrics
	tmpl    *template.Template
}

// NewDashboard creates a new web dashboard
func NewDashboard(server *Server, logger forge.Logger, metrics forge.Metrics) *Dashboard {
	tmpl := template.Must(template.New("dashboard").Parse(dashboardHTML))
	
	return &Dashboard{
		server:  server,
		logger:  logger,
		metrics: metrics,
		tmpl:    tmpl,
	}
}

// MountRoutes mounts the dashboard routes
func (d *Dashboard) MountRoutes(router forge.Router, basePath string) error {
	// Dashboard home
	router.GET(basePath, d.handleDashboard,
		forge.WithSummary("Dashboard Home"),
	)

	// Dashboard sections
	router.GET(basePath+"/generation", d.handleGeneration,
		forge.WithSummary("Generation Playground"),
	)

	router.GET(basePath+"/agents", d.handleAgents,
		forge.WithSummary("Agent Management"),
	)

	router.GET(basePath+"/rag", d.handleRAG,
		forge.WithSummary("RAG Dashboard"),
	)

	router.GET(basePath+"/costs", d.handleCosts,
		forge.WithSummary("Cost Analytics"),
	)

	router.GET(basePath+"/metrics", d.handleMetrics,
		forge.WithSummary("Metrics Dashboard"),
	)

	if d.logger != nil {
		d.logger.Info("SDK Dashboard routes mounted", forge.F("base_path", basePath))
	}

	return nil
}

func (d *Dashboard) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":   "AI SDK Dashboard",
		"Section": "home",
	}
	d.tmpl.Execute(w, data)
}

func (d *Dashboard) handleGeneration(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":   "Generation Playground",
		"Section": "generation",
	}
	d.tmpl.Execute(w, data)
}

func (d *Dashboard) handleAgents(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":   "Agent Management",
		"Section": "agents",
	}
	d.tmpl.Execute(w, data)
}

func (d *Dashboard) handleRAG(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":   "RAG Dashboard",
		"Section": "rag",
	}
	d.tmpl.Execute(w, data)
}

func (d *Dashboard) handleCosts(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":   "Cost Analytics",
		"Section": "costs",
	}
	
	// Get cost insights if available
	if d.server.costManager != nil {
		insights := d.server.costManager.GetInsights()
		data["Insights"] = insights
	}
	
	d.tmpl.Execute(w, data)
}

func (d *Dashboard) handleMetrics(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title":   "Metrics Dashboard",
		"Section": "metrics",
	}
	d.tmpl.Execute(w, data)
}

// dashboardHTML is the main dashboard template with Tailwind CSS
const dashboardHTML = `<!DOCTYPE html>
<html lang="en" class="h-full bg-gray-50">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}} - AI SDK</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <script>
        tailwind.config = {
            theme: {
                extend: {
                    colors: {
                        primary: '#3b82f6',
                        secondary: '#8b5cf6',
                    }
                }
            }
        }
    </script>
</head>
<body class="h-full">
    <div class="min-h-full">
        <!-- Navigation -->
        <nav class="bg-white shadow-sm">
            <div class="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
                <div class="flex h-16 justify-between">
                    <div class="flex">
                        <div class="flex flex-shrink-0 items-center">
                            <h1 class="text-2xl font-bold text-primary">AI SDK</h1>
                        </div>
                        <div class="hidden sm:ml-6 sm:flex sm:space-x-8">
                            <a href="/dashboard" class="inline-flex items-center border-b-2 px-1 pt-1 text-sm font-medium {{if eq .Section "home"}}border-primary text-gray-900{{else}}border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700{{end}}">
                                Dashboard
                            </a>
                            <a href="/dashboard/generation" class="inline-flex items-center border-b-2 px-1 pt-1 text-sm font-medium {{if eq .Section "generation"}}border-primary text-gray-900{{else}}border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700{{end}}">
                                Generation
                            </a>
                            <a href="/dashboard/agents" class="inline-flex items-center border-b-2 px-1 pt-1 text-sm font-medium {{if eq .Section "agents"}}border-primary text-gray-900{{else}}border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700{{end}}">
                                Agents
                            </a>
                            <a href="/dashboard/rag" class="inline-flex items-center border-b-2 px-1 pt-1 text-sm font-medium {{if eq .Section "rag"}}border-primary text-gray-900{{else}}border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700{{end}}">
                                RAG
                            </a>
                            <a href="/dashboard/costs" class="inline-flex items-center border-b-2 px-1 pt-1 text-sm font-medium {{if eq .Section "costs"}}border-primary text-gray-900{{else}}border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700{{end}}">
                                Costs
                            </a>
                            <a href="/dashboard/metrics" class="inline-flex items-center border-b-2 px-1 pt-1 text-sm font-medium {{if eq .Section "metrics"}}border-primary text-gray-900{{else}}border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700{{end}}">
                                Metrics
                            </a>
                        </div>
                    </div>
                </div>
            </div>
        </nav>

        <!-- Main Content -->
        <div class="py-10">
            <main>
                <div class="mx-auto max-w-7xl sm:px-6 lg:px-8">
                    {{if eq .Section "home"}}
                        <!-- Home Dashboard -->
                        <div class="px-4 sm:px-0">
                            <h2 class="text-3xl font-bold tracking-tight text-gray-900">Overview</h2>
                            <p class="mt-2 text-sm text-gray-700">Monitor and manage your AI SDK</p>
                        </div>

                        <!-- Stats Grid -->
                        <div class="mt-8 grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-4">
                            <div class="overflow-hidden rounded-lg bg-white px-4 py-5 shadow sm:p-6">
                                <dt class="truncate text-sm font-medium text-gray-500">Total Requests</dt>
                                <dd class="mt-1 text-3xl font-semibold tracking-tight text-gray-900">12,345</dd>
                            </div>
                            <div class="overflow-hidden rounded-lg bg-white px-4 py-5 shadow sm:p-6">
                                <dt class="truncate text-sm font-medium text-gray-500">Active Agents</dt>
                                <dd class="mt-1 text-3xl font-semibold tracking-tight text-gray-900">8</dd>
                            </div>
                            <div class="overflow-hidden rounded-lg bg-white px-4 py-5 shadow sm:p-6">
                                <dt class="truncate text-sm font-medium text-gray-500">Total Cost</dt>
                                <dd class="mt-1 text-3xl font-semibold tracking-tight text-gray-900">$45.67</dd>
                            </div>
                            <div class="overflow-hidden rounded-lg bg-white px-4 py-5 shadow sm:p-6">
                                <dt class="truncate text-sm font-medium text-gray-500">Success Rate</dt>
                                <dd class="mt-1 text-3xl font-semibold tracking-tight text-gray-900">99.2%</dd>
                            </div>
                        </div>

                        <!-- Recent Activity -->
                        <div class="mt-8">
                            <h3 class="text-lg font-medium leading-6 text-gray-900">Recent Activity</h3>
                            <div class="mt-4 bg-white shadow sm:rounded-lg">
                                <ul role="list" class="divide-y divide-gray-200">
                                    <li class="px-4 py-4 sm:px-6">
                                        <div class="flex items-center justify-between">
                                            <p class="truncate text-sm font-medium text-primary">Text Generation</p>
                                            <div class="ml-2 flex flex-shrink-0">
                                                <p class="inline-flex rounded-full bg-green-100 px-2 text-xs font-semibold leading-5 text-green-800">Success</p>
                                            </div>
                                        </div>
                                        <div class="mt-2 sm:flex sm:justify-between">
                                            <div class="sm:flex">
                                                <p class="flex items-center text-sm text-gray-500">Model: gpt-4</p>
                                            </div>
                                            <div class="mt-2 flex items-center text-sm text-gray-500 sm:mt-0">
                                                <p>2 minutes ago</p>
                                            </div>
                                        </div>
                                    </li>
                                </ul>
                            </div>
                        </div>
                    {{else if eq .Section "generation"}}
                        <!-- Generation Playground -->
                        <div class="px-4 sm:px-0">
                            <h2 class="text-3xl font-bold tracking-tight text-gray-900">Generation Playground</h2>
                            <p class="mt-2 text-sm text-gray-700">Test text generation with different models</p>
                        </div>

                        <div class="mt-8 bg-white shadow sm:rounded-lg p-6">
                            <form id="generation-form" class="space-y-6">
                                <div>
                                    <label for="model" class="block text-sm font-medium text-gray-700">Model</label>
                                    <select id="model" name="model" class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-primary focus:ring-primary sm:text-sm">
                                        <option>gpt-4</option>
                                        <option>gpt-3.5-turbo</option>
                                        <option>claude-3-opus</option>
                                    </select>
                                </div>

                                <div>
                                    <label for="prompt" class="block text-sm font-medium text-gray-700">Prompt</label>
                                    <textarea id="prompt" name="prompt" rows="4" class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-primary focus:ring-primary sm:text-sm" placeholder="Enter your prompt..."></textarea>
                                </div>

                                <div class="grid grid-cols-2 gap-4">
                                    <div>
                                        <label for="temperature" class="block text-sm font-medium text-gray-700">Temperature</label>
                                        <input type="number" id="temperature" name="temperature" min="0" max="2" step="0.1" value="0.7" class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-primary focus:ring-primary sm:text-sm">
                                    </div>
                                    <div>
                                        <label for="max_tokens" class="block text-sm font-medium text-gray-700">Max Tokens</label>
                                        <input type="number" id="max_tokens" name="max_tokens" value="1000" class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-primary focus:ring-primary sm:text-sm">
                                    </div>
                                </div>

                                <div>
                                    <button type="submit" class="inline-flex justify-center rounded-md border border-transparent bg-primary px-4 py-2 text-sm font-medium text-white shadow-sm hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2">
                                        Generate
                                    </button>
                                </div>

                                <div id="result" class="hidden">
                                    <label class="block text-sm font-medium text-gray-700">Result</label>
                                    <div class="mt-1 block w-full rounded-md border border-gray-300 bg-gray-50 p-4 text-sm"></div>
                                </div>
                            </form>
                        </div>
                    {{else if eq .Section "costs"}}
                        <!-- Cost Analytics -->
                        <div class="px-4 sm:px-0">
                            <h2 class="text-3xl font-bold tracking-tight text-gray-900">Cost Analytics</h2>
                            <p class="mt-2 text-sm text-gray-700">Monitor API costs and optimize spending</p>
                        </div>

                        {{if .Insights}}
                        <div class="mt-8 grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
                            <div class="overflow-hidden rounded-lg bg-white px-4 py-5 shadow sm:p-6">
                                <dt class="truncate text-sm font-medium text-gray-500">Today</dt>
                                <dd class="mt-1 text-3xl font-semibold tracking-tight text-gray-900">${{.Insights.CostToday | printf "%.2f"}}</dd>
                            </div>
                            <div class="overflow-hidden rounded-lg bg-white px-4 py-5 shadow sm:p-6">
                                <dt class="truncate text-sm font-medium text-gray-500">This Month</dt>
                                <dd class="mt-1 text-3xl font-semibold tracking-tight text-gray-900">${{.Insights.CostThisMonth | printf "%.2f"}}</dd>
                            </div>
                            <div class="overflow-hidden rounded-lg bg-white px-4 py-5 shadow sm:p-6">
                                <dt class="truncate text-sm font-medium text-gray-500">Projected</dt>
                                <dd class="mt-1 text-3xl font-semibold tracking-tight text-gray-900">${{.Insights.ProjectedMonthly | printf "%.2f"}}</dd>
                            </div>
                        </div>
                        {{end}}
                    {{end}}
                </div>
            </main>
        </div>
    </div>

    <script>
        // Generation form handler
        const form = document.getElementById('generation-form');
        if (form) {
            form.addEventListener('submit', async (e) => {
                e.preventDefault();
                const formData = new FormData(form);
                const data = Object.fromEntries(formData);
                
                try {
                    const response = await fetch('/api/ai/sdk/generate', {
                        method: 'POST',
                        headers: {'Content-Type': 'application/json'},
                        body: JSON.stringify(data)
                    });
                    
                    const result = await response.json();
                    const resultDiv = document.getElementById('result');
                    resultDiv.classList.remove('hidden');
                    resultDiv.querySelector('div').textContent = result.content;
                } catch (error) {
                    alert('Error: ' + error.message);
                }
            });
        }
    </script>
</body>
</html>`


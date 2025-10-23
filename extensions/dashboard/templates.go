package dashboard

import (
	"fmt"
)

// generateHTML returns the complete dashboard HTML
func (ds *DashboardServer) generateHTML() string {
	theme := ds.config.Theme
	title := ds.config.Title
	basePath := ds.config.BasePath
	enableRealtime := ds.config.EnableRealtime

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en" class="h-full">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <script src="https://cdn.jsdelivr.net/npm/apexcharts@3.45.0/dist/apexcharts.min.js"></script>
    <script>
        // Tailwind dark mode configuration
        tailwind.config = {
            darkMode: 'class'
        }
    </script>
    <style>
        [x-cloak] { display: none !important; }
        .status-healthy { color: #10b981; }
        .status-degraded { color: #f59e0b; }
        .status-unhealthy { color: #ef4444; }
        .status-unknown { color: #6b7280; }
        .bg-healthy { background-color: #10b981; }
        .bg-degraded { background-color: #f59e0b; }
        .bg-unhealthy { background-color: #ef4444; }
        .bg-unknown { background-color: #6b7280; }
        .modal { display: none; }
        .modal.active { display: flex; }
    </style>
</head>
<body class="h-full bg-gray-50 dark:bg-gray-900 transition-colors">
    <div id="app" class="min-h-screen">
        <!-- Header -->
        <header class="bg-white dark:bg-gray-800 shadow transition-colors">
            <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4">
                <div class="flex justify-between items-center">
                    <div>
                        <h1 class="text-3xl font-bold text-gray-900 dark:text-white">%s</h1>
                        <p class="text-sm text-gray-500 dark:text-gray-400 mt-1">Real-time health and metrics monitoring</p>
                    </div>
                    <div class="flex items-center space-x-4">
                        <div id="ws-status" class="flex items-center space-x-2">
                            <div class="w-2 h-2 rounded-full bg-gray-400 dark:bg-gray-600" id="ws-indicator"></div>
                            <span class="text-sm text-gray-600 dark:text-gray-300" id="ws-text">Connecting...</span>
                        </div>
                        <button id="theme-toggle" class="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors">
                            <svg id="theme-icon-dark" class="w-6 h-6 text-gray-600 dark:text-gray-300 hidden dark:block" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z"></path>
                            </svg>
                            <svg id="theme-icon-light" class="w-6 h-6 text-gray-600 dark:text-gray-300 block dark:hidden" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z"></path>
                            </svg>
                        </button>
                    </div>
                </div>
            </div>
        </header>

        <!-- Main Content -->
        <main class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
            <!-- Tab Navigation -->
            <div class="mb-6 border-b border-gray-200 dark:border-gray-700">
                <nav class="-mb-px flex space-x-8">
                    <button onclick="switchTab('overview')" id="tab-overview" class="tab-button border-blue-500 text-blue-600 dark:text-blue-400 whitespace-nowrap py-4 px-1 border-b-2 font-medium text-sm">
                        Overview
                    </button>
                    <button onclick="switchTab('metrics')" id="tab-metrics" class="tab-button border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600 whitespace-nowrap py-4 px-1 border-b-2 font-medium text-sm">
                        Metrics Report
                    </button>
                </nav>
            </div>

            <!-- Overview Tab Content -->
            <div id="content-overview" class="tab-content">
                <!-- Overview Cards -->
                <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8">
                    <!-- Health Status Card -->
                    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6 transition-colors">
                        <div class="flex items-center justify-between">
                            <div>
                                <p class="text-sm font-medium text-gray-600 dark:text-gray-400">Overall Health</p>
                                <p id="overall-health" class="text-2xl font-bold mt-2 status-healthy">Healthy</p>
                            </div>
                            <div class="w-12 h-12 bg-green-100 dark:bg-green-900 rounded-lg flex items-center justify-center">
                                <svg class="w-6 h-6 text-green-600 dark:text-green-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"></path>
                                </svg>
                            </div>
                        </div>
                        <p class="text-xs text-gray-500 dark:text-gray-400 mt-4">Last checked: <span id="last-check">--</span></p>
                    </div>

                    <!-- Services Card -->
                    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6 transition-colors">
                        <div class="flex items-center justify-between">
                            <div>
                                <p class="text-sm font-medium text-gray-600 dark:text-gray-400">Services</p>
                                <p class="text-2xl font-bold text-gray-900 dark:text-white mt-2">
                                    <span id="healthy-services">0</span>/<span id="total-services">0</span>
                                </p>
                            </div>
                            <div class="w-12 h-12 bg-blue-100 dark:bg-blue-900 rounded-lg flex items-center justify-center">
                                <svg class="w-6 h-6 text-blue-600 dark:text-blue-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01"></path>
                                </svg>
                            </div>
                        </div>
                        <p class="text-xs text-gray-500 dark:text-gray-400 mt-4">Healthy services</p>
                    </div>

                    <!-- Metrics Card -->
                    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6 transition-colors">
                        <div class="flex items-center justify-between">
                            <div>
                                <p class="text-sm font-medium text-gray-600 dark:text-gray-400">Metrics</p>
                                <p id="total-metrics" class="text-2xl font-bold text-gray-900 dark:text-white mt-2">0</p>
                            </div>
                            <div class="w-12 h-12 bg-purple-100 dark:bg-purple-900 rounded-lg flex items-center justify-center">
                                <svg class="w-6 h-6 text-purple-600 dark:text-purple-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"></path>
                                </svg>
                            </div>
                        </div>
                        <p class="text-xs text-gray-500 dark:text-gray-400 mt-4">Total metrics tracked</p>
                    </div>

                    <!-- Uptime Card -->
                    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6 transition-colors">
                        <div class="flex items-center justify-between">
                            <div>
                                <p class="text-sm font-medium text-gray-600 dark:text-gray-400">Uptime</p>
                                <p id="uptime" class="text-2xl font-bold text-gray-900 dark:text-white mt-2">--</p>
                            </div>
                            <div class="w-12 h-12 bg-yellow-100 dark:bg-yellow-900 rounded-lg flex items-center justify-center">
                                <svg class="w-6 h-6 text-yellow-600 dark:text-yellow-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"></path>
                                </svg>
                            </div>
                        </div>
                        <p class="text-xs text-gray-500 dark:text-gray-400 mt-4">System uptime</p>
                    </div>
                </div>

                <!-- Charts Section -->
                <div class="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-8">
                    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6 transition-colors">
                        <h3 class="text-lg font-semibold text-gray-900 dark:text-white mb-4">Service Health History</h3>
                        <div id="health-chart" class="h-64"></div>
                    </div>
                    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6 transition-colors">
                        <h3 class="text-lg font-semibold text-gray-900 dark:text-white mb-4">Service Count History</h3>
                        <div id="services-chart" class="h-64"></div>
                    </div>
                </div>

                <!-- Health Checks Table -->
                <div class="bg-white dark:bg-gray-800 rounded-lg shadow mb-8 transition-colors">
                    <div class="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
                        <h3 class="text-lg font-semibold text-gray-900 dark:text-white">Health Checks</h3>
                    </div>
                    <div class="overflow-x-auto">
                        <table class="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
                            <thead class="bg-gray-50 dark:bg-gray-900">
                                <tr>
                                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">Service</th>
                                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">Status</th>
                                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">Message</th>
                                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">Duration</th>
                                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">Checked At</th>
                                </tr>
                            </thead>
                            <tbody id="health-table" class="bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700">
                                <tr>
                                    <td colspan="5" class="px-6 py-4 text-center text-sm text-gray-500 dark:text-gray-400">Loading...</td>
                                </tr>
                            </tbody>
                        </table>
                    </div>
                </div>

                <!-- Services List -->
                <div class="bg-white dark:bg-gray-800 rounded-lg shadow transition-colors">
                    <div class="px-6 py-4 border-b border-gray-200 dark:border-gray-700 flex justify-between items-center">
                        <h3 class="text-lg font-semibold text-gray-900 dark:text-white">Registered Services</h3>
                        <div class="flex space-x-2">
                            <a href="%s/export/json" class="text-sm px-3 py-1 bg-blue-500 text-white rounded hover:bg-blue-600 transition-colors">Export JSON</a>
                            <a href="%s/export/csv" class="text-sm px-3 py-1 bg-green-500 text-white rounded hover:bg-green-600 transition-colors">Export CSV</a>
                            <a href="%s/export/prometheus" class="text-sm px-3 py-1 bg-purple-500 text-white rounded hover:bg-purple-600 transition-colors">Prometheus</a>
                        </div>
                    </div>
                    <div class="p-6">
                        <div id="services-list" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                            <div class="text-center text-sm text-gray-500 dark:text-gray-400">Loading...</div>
                        </div>
                    </div>
                </div>
            </div>

            <!-- Metrics Report Tab Content -->
            <div id="content-metrics" class="tab-content hidden">
                <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6 mb-6 transition-colors">
                    <h3 class="text-lg font-semibold text-gray-900 dark:text-white mb-4">Metrics Statistics</h3>
                    <div class="grid grid-cols-1 md:grid-cols-4 gap-4" id="metrics-stats">
                        <div class="text-center text-sm text-gray-500 dark:text-gray-400">Loading...</div>
                    </div>
                </div>

                <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6 mb-6 transition-colors">
                    <h3 class="text-lg font-semibold text-gray-900 dark:text-white mb-4">Metrics by Type</h3>
                    <div id="metrics-by-type" class="h-64"></div>
                </div>

                <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6 mb-6 transition-colors">
                    <h3 class="text-lg font-semibold text-gray-900 dark:text-white mb-4">Active Collectors</h3>
                    <div id="collectors-list" class="overflow-x-auto">
                        <table class="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
                            <thead>
                                <tr>
                                    <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Name</th>
                                    <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Type</th>
                                    <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Metrics</th>
                                    <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Status</th>
                                </tr>
                            </thead>
                            <tbody id="collectors-table" class="divide-y divide-gray-200 dark:divide-gray-700">
                                <tr><td colspan="4" class="px-4 py-2 text-center text-sm text-gray-500 dark:text-gray-400">Loading...</td></tr>
                            </tbody>
                        </table>
                    </div>
                </div>

                <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6 transition-colors">
                    <h3 class="text-lg font-semibold text-gray-900 dark:text-white mb-4">Top Metrics</h3>
                    <div id="top-metrics-list" class="space-y-2">
                        <div class="text-center text-sm text-gray-500 dark:text-gray-400">Loading...</div>
                    </div>
                </div>
            </div>
        </main>

        <!-- Service Detail Modal -->
        <div id="service-modal" class="modal fixed inset-0 bg-black bg-opacity-50 items-center justify-center z-50">
            <div class="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-4xl w-full mx-4 max-h-[90vh] overflow-y-auto transition-colors">
                <div class="px-6 py-4 border-b border-gray-200 dark:border-gray-700 flex justify-between items-center sticky top-0 bg-white dark:bg-gray-800">
                    <h3 class="text-xl font-semibold text-gray-900 dark:text-white" id="modal-service-name">Service Details</h3>
                    <button onclick="closeServiceModal()" class="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300">
                        <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
                        </svg>
                    </button>
                </div>
                <div class="p-6" id="modal-service-content">
                    <div class="text-center text-sm text-gray-500 dark:text-gray-400">Loading...</div>
                </div>
            </div>
        </div>

        <!-- Footer -->
        <footer class="bg-white dark:bg-gray-800 shadow mt-8 transition-colors">
            <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4">
                <p class="text-center text-sm text-gray-500 dark:text-gray-400">
                    Powered by Forge Dashboard v2.0.0 | Last updated: <span id="last-updated">--</span>
                </p>
            </div>
        </footer>
    </div>

    <script>
        const state = {
            ws: null,
            reconnectAttempts: 0,
            maxReconnectAttempts: 5,
            reconnectDelay: 2000,
            charts: {},
            data: {
                overview: null,
                health: null,
                services: null,
                history: null,
                metricsReport: null
            },
            currentTab: 'overview'
        };

        document.addEventListener('DOMContentLoaded', () => {
            initTheme();
            initCharts();
            fetchData();
            %s
            setInterval(fetchData, 30000);
        });

        function initTheme() {
            const configTheme = '%s';
            const savedTheme = localStorage.getItem('dashboard-theme');
            
            let isDark = false;
            if (savedTheme === 'dark') {
                isDark = true;
            } else if (savedTheme === 'light') {
                isDark = false;
            } else if (configTheme === 'dark') {
                isDark = true;
            } else if (configTheme === 'auto' && window.matchMedia('(prefers-color-scheme: dark)').matches) {
                isDark = true;
            }
            
            if (isDark) {
                document.documentElement.classList.add('dark');
            }

            document.getElementById('theme-toggle').addEventListener('click', () => {
                const isDarkNow = document.documentElement.classList.contains('dark');
                if (isDarkNow) {
                    document.documentElement.classList.remove('dark');
                    localStorage.setItem('dashboard-theme', 'light');
                } else {
                    document.documentElement.classList.add('dark');
                    localStorage.setItem('dashboard-theme', 'dark');
                }
                updateChartThemes();
            });
        }
        
        function updateChartThemes() {
            const isDark = document.documentElement.classList.contains('dark');
            const theme = isDark ? 'dark' : 'light';
            
            if (state.charts.health) {
                state.charts.health.updateOptions({ theme: { mode: theme } });
            }
            if (state.charts.services) {
                state.charts.services.updateOptions({ theme: { mode: theme } });
            }
            if (state.charts.metricsType) {
                state.charts.metricsType.updateOptions({ theme: { mode: theme } });
            }
        }

        function switchTab(tab) {
            state.currentTab = tab;
            
            document.querySelectorAll('.tab-button').forEach(btn => {
                btn.classList.remove('border-blue-500', 'text-blue-600', 'dark:text-blue-400');
                btn.classList.add('border-transparent', 'text-gray-500', 'dark:text-gray-400');
            });
            
            document.getElementById('tab-' + tab).classList.remove('border-transparent', 'text-gray-500', 'dark:text-gray-400');
            document.getElementById('tab-' + tab).classList.add('border-blue-500', 'text-blue-600', 'dark:text-blue-400');
            
            document.querySelectorAll('.tab-content').forEach(content => {
                content.classList.add('hidden');
            });
            
            document.getElementById('content-' + tab).classList.remove('hidden');
            
            if (tab === 'metrics' && !state.data.metricsReport) {
                fetchMetricsReport();
            }
        }

        function initCharts() {
            const isDark = document.documentElement.classList.contains('dark');
            const chartOptions = {
                chart: { type: 'line', height: 256, toolbar: { show: false }, animations: { enabled: true } },
                theme: { mode: isDark ? 'dark' : 'light' },
                stroke: { curve: 'smooth', width: 2 },
                grid: { borderColor: isDark ? '#374151' : '#e5e7eb' },
                xaxis: { type: 'datetime', labels: { style: { colors: isDark ? '#9ca3af' : '#6b7280' } } },
                yaxis: { labels: { style: { colors: isDark ? '#9ca3af' : '#6b7280' } } },
                tooltip: { theme: isDark ? 'dark' : 'light' }
            };

            state.charts.health = new ApexCharts(document.querySelector("#health-chart"), {
                ...chartOptions,
                series: [{ name: 'Healthy', data: [] }, { name: 'Degraded', data: [] }, { name: 'Unhealthy', data: [] }],
                colors: ['#10b981', '#f59e0b', '#ef4444']
            });
            state.charts.health.render();

            state.charts.services = new ApexCharts(document.querySelector("#services-chart"), {
                ...chartOptions,
                series: [{ name: 'Total Services', data: [] }, { name: 'Healthy Services', data: [] }],
                colors: ['#3b82f6', '#10b981']
            });
            state.charts.services.render();
        }

        async function fetchData() {
            try {
                const [overview, health, services, history] = await Promise.all([
                    fetch('%s/api/overview').then(r => r.json()),
                    fetch('%s/api/health').then(r => r.json()),
                    fetch('%s/api/services').then(r => r.json()),
                    fetch('%s/api/history').then(r => r.json())
                ]);

                state.data = { ...state.data, overview, health, services, history };
                updateUI();
            } catch (error) {
                console.error('Failed to fetch data:', error);
            }
        }

        async function fetchMetricsReport() {
            try {
                const report = await fetch('%s/api/metrics-report').then(r => r.json());
                state.data.metricsReport = report;
                updateMetricsReport();
            } catch (error) {
                console.error('Failed to fetch metrics report:', error);
            }
        }

        function updateUI() {
            const { overview, health, services, history } = state.data;

            if (overview) {
                document.getElementById('overall-health').textContent = overview.overall_health;
                document.getElementById('overall-health').className = 'text-2xl font-bold mt-2 status-' + overview.overall_health.toLowerCase();
                document.getElementById('healthy-services').textContent = overview.healthy_services;
                document.getElementById('total-services').textContent = overview.total_services;
                document.getElementById('total-metrics').textContent = overview.total_metrics;
                document.getElementById('uptime').textContent = formatDuration(overview.uptime);
                document.getElementById('last-updated').textContent = new Date(overview.timestamp).toLocaleTimeString();
            }

            if (health) {
                updateHealthTable(health);
                document.getElementById('last-check').textContent = new Date(health.checked_at).toLocaleTimeString();
            }

            if (services) {
                updateServicesList(services);
            }

            if (history) {
                updateCharts(history);
            }
        }

        function updateHealthTable(health) {
            const tbody = document.getElementById('health-table');
            tbody.innerHTML = '';

            for (const [name, service] of Object.entries(health.services)) {
                const row = document.createElement('tr');
                row.className = 'hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors';
                row.innerHTML = '<td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900 dark:text-white">' + name + '</td>' +
                    '<td class="px-6 py-4 whitespace-nowrap">' +
                    '<span class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full bg-' + getStatusColor(service.status) + '-100 text-' + getStatusColor(service.status) + '-800">' +
                    service.status +
                    '</span>' +
                    '</td>' +
                    '<td class="px-6 py-4 text-sm text-gray-500 dark:text-gray-400">' + (service.message || '-') + '</td>' +
                    '<td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500 dark:text-gray-400">' + formatDuration(service.duration) + '</td>' +
                    '<td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500 dark:text-gray-400">' + new Date(service.timestamp).toLocaleTimeString() + '</td>';
                tbody.appendChild(row);
            }
        }

        function updateServicesList(services) {
            const container = document.getElementById('services-list');
            container.innerHTML = '';

            services.forEach(service => {
                const div = document.createElement('div');
                div.className = 'p-4 bg-gray-50 dark:bg-gray-700 rounded-lg border-l-4 border-' + getStatusColor(service.status) + '-500 cursor-pointer hover:shadow-md transition-all';
                div.onclick = () => showServiceDetail(service.name);
                div.innerHTML = '<div class="flex items-center justify-between">' +
                    '<span class="text-sm font-medium text-gray-900 dark:text-white">' + service.name + '</span>' +
                    '<span class="px-2 py-1 text-xs bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 rounded">' + service.type + '</span>' +
                    '</div>' +
                    '<div class="flex items-center mt-2">' +
                    '<span class="inline-flex items-center px-2 py-1 text-xs font-medium rounded-full bg-' + getStatusColor(service.status) + '-100 text-' + getStatusColor(service.status) + '-800">' +
                    '<span class="w-1.5 h-1.5 mr-1.5 rounded-full bg-' + getStatusColor(service.status) + '-600"></span>' +
                    service.status +
                    '</span>' +
                    '</div>';
                container.appendChild(div);
            });
        }

        function updateCharts(history) {
            if (!history.series || history.series.length === 0) return;

            const serviceSeries = history.series.filter(s => s.name === 'service_count' || s.name === 'healthy_services');
            if (serviceSeries.length > 0) {
                const seriesData = serviceSeries.map(series => ({
                    name: series.name === 'service_count' ? 'Total Services' : 'Healthy Services',
                    data: series.points.map(p => ({ x: new Date(p.timestamp), y: p.value }))
                }));
                state.charts.services.updateSeries(seriesData);
            }
        }

        function updateMetricsReport() {
            const report = state.data.metricsReport;
            if (!report) return;

            const statsContainer = document.getElementById('metrics-stats');
            statsContainer.innerHTML = '';
            
            [
                { label: 'Total Metrics', value: report.total_metrics },
                { label: 'Counters', value: report.metrics_by_type.counter || 0 },
                { label: 'Gauges', value: report.metrics_by_type.gauge || 0 },
                { label: 'Histograms', value: report.metrics_by_type.histogram || 0 }
            ].forEach(stat => {
                const div = document.createElement('div');
                div.className = 'text-center p-4 bg-gray-50 dark:bg-gray-700 rounded-lg';
                div.innerHTML = '<p class="text-sm text-gray-500 dark:text-gray-400">' + stat.label + '</p>' +
                    '<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">' + stat.value + '</p>';
                statsContainer.appendChild(div);
            });

            const collectorsTable = document.getElementById('collectors-table');
            collectorsTable.innerHTML = '';
            report.collectors.forEach(collector => {
                const row = document.createElement('tr');
                row.className = 'hover:bg-gray-50 dark:hover:bg-gray-700';
                row.innerHTML = '<td class="px-4 py-2 text-sm text-gray-900 dark:text-white">' + collector.name + '</td>' +
                    '<td class="px-4 py-2 text-sm text-gray-500 dark:text-gray-400">' + collector.type + '</td>' +
                    '<td class="px-4 py-2 text-sm text-gray-500 dark:text-gray-400">' + collector.metrics_count + '</td>' +
                    '<td class="px-4 py-2"><span class="px-2 py-1 text-xs rounded-full bg-green-100 text-green-800">' + collector.status + '</span></td>';
                collectorsTable.appendChild(row);
            });

            const topMetricsList = document.getElementById('top-metrics-list');
            topMetricsList.innerHTML = '';
            report.top_metrics.slice(0, 10).forEach(metric => {
                const div = document.createElement('div');
                div.className = 'flex justify-between items-center p-3 bg-gray-50 dark:bg-gray-700 rounded';
                div.innerHTML = '<div><span class="text-sm font-medium text-gray-900 dark:text-white">' + metric.name + '</span>' +
                    '<span class="ml-2 text-xs text-gray-500 dark:text-gray-400">(' + metric.type + ')</span></div>' +
                    '<span class="text-sm font-mono text-gray-700 dark:text-gray-300">' + JSON.stringify(metric.value) + '</span>';
                topMetricsList.appendChild(div);
            });

            if (!state.charts.metricsType && report.metrics_by_type) {
                const isDark = document.documentElement.classList.contains('dark');
                state.charts.metricsType = new ApexCharts(document.querySelector("#metrics-by-type"), {
                    chart: { type: 'pie', height: 256 },
                    theme: { mode: isDark ? 'dark' : 'light' },
                    series: Object.values(report.metrics_by_type),
                    labels: Object.keys(report.metrics_by_type),
                    colors: ['#3b82f6', '#10b981', '#f59e0b', '#8b5cf6']
                });
                state.charts.metricsType.render();
            }
        }

        async function showServiceDetail(serviceName) {
            document.getElementById('modal-service-name').textContent = serviceName;
            document.getElementById('service-modal').classList.add('active');
            
            try {
                const detail = await fetch('%s/api/service-detail?name=' + encodeURIComponent(serviceName)).then(r => r.json());
                
                const content = document.getElementById('modal-service-content');
                content.innerHTML = '';
                
                const sections = [
                    { title: 'Status', value: detail.status, color: getStatusColor(detail.status) },
                    { title: 'Type', value: detail.type },
                    { title: 'Last Health Check', value: detail.last_health_check ? new Date(detail.last_health_check).toLocaleString() : 'N/A' }
                ];
                
                sections.forEach(section => {
                    const div = document.createElement('div');
                    div.className = 'mb-4';
                    div.innerHTML = '<h4 class="text-sm font-medium text-gray-500 dark:text-gray-400 mb-1">' + section.title + '</h4>' +
                        '<p class="text-lg font-semibold text-gray-900 dark:text-white">' + section.value + '</p>';
                    content.appendChild(div);
                });
                
                if (detail.health) {
                    const healthDiv = document.createElement('div');
                    healthDiv.className = 'mt-6';
                    healthDiv.innerHTML = '<h4 class="text-lg font-semibold text-gray-900 dark:text-white mb-3">Health Details</h4>' +
                        '<div class="bg-gray-50 dark:bg-gray-700 p-4 rounded"><p class="text-sm text-gray-700 dark:text-gray-300">' + 
                        (detail.health.message || 'No message') + '</p></div>';
                    content.appendChild(healthDiv);
                }
                
                if (Object.keys(detail.metrics).length > 0) {
                    const metricsDiv = document.createElement('div');
                    metricsDiv.className = 'mt-6';
                    metricsDiv.innerHTML = '<h4 class="text-lg font-semibold text-gray-900 dark:text-white mb-3">Metrics (' + Object.keys(detail.metrics).length + ')</h4>' +
                        '<div class="space-y-2 max-h-96 overflow-y-auto">' +
                        Object.entries(detail.metrics).map(([key, value]) => 
                            '<div class="flex justify-between items-center p-2 bg-gray-50 dark:bg-gray-700 rounded">' +
                            '<span class="text-sm font-mono text-gray-700 dark:text-gray-300">' + key + '</span>' +
                            '<span class="text-sm font-semibold text-gray-900 dark:text-white">' + JSON.stringify(value) + '</span></div>'
                        ).join('') +
                        '</div>';
                    content.appendChild(metricsDiv);
                } else {
                    const noMetrics = document.createElement('div');
                    noMetrics.className = 'mt-6 text-center text-gray-500 dark:text-gray-400';
                    noMetrics.textContent = 'No metrics available for this service';
                    content.appendChild(noMetrics);
                }
            } catch (error) {
                console.error('Failed to load service details:', error);
                document.getElementById('modal-service-content').innerHTML = '<p class="text-red-500">Failed to load service details</p>';
            }
        }

        function closeServiceModal() {
            document.getElementById('service-modal').classList.remove('active');
        }

        function initWebSocket() {
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const wsUrl = protocol + '//' + window.location.host + '%s/ws';

            state.ws = new WebSocket(wsUrl);

            state.ws.onopen = () => {
                console.log('WebSocket connected');
                state.reconnectAttempts = 0;
                updateWSStatus('connected');
            };

            state.ws.onmessage = (event) => {
                try {
                    const message = JSON.parse(event.data);
                    handleWSMessage(message);
                } catch (error) {
                    console.error('Failed to parse WebSocket message:', error);
                }
            };

            state.ws.onerror = (error) => {
                console.error('WebSocket error:', error);
                updateWSStatus('error');
            };

            state.ws.onclose = () => {
                console.log('WebSocket closed');
                updateWSStatus('disconnected');
                reconnectWebSocket();
            };
        }

        function handleWSMessage(message) {
            if (message.type === 'overview') {
                state.data.overview = message.data;
            } else if (message.type === 'health') {
                state.data.health = message.data;
            }
            updateUI();
        }

        function reconnectWebSocket() {
            if (state.reconnectAttempts >= state.maxReconnectAttempts) {
                console.log('Max reconnection attempts reached');
                return;
            }

            state.reconnectAttempts++;
            setTimeout(() => {
                console.log('Reconnecting WebSocket (attempt ' + state.reconnectAttempts + ')...');
                initWebSocket();
            }, state.reconnectDelay * state.reconnectAttempts);
        }

        function updateWSStatus(status) {
            const indicator = document.getElementById('ws-indicator');
            const text = document.getElementById('ws-text');

            switch (status) {
                case 'connected':
                    indicator.className = 'w-2 h-2 rounded-full bg-green-500';
                    text.textContent = 'Live';
                    break;
                case 'disconnected':
                    indicator.className = 'w-2 h-2 rounded-full bg-gray-400';
                    text.textContent = 'Disconnected';
                    break;
                case 'error':
                    indicator.className = 'w-2 h-2 rounded-full bg-red-500';
                    text.textContent = 'Error';
                    break;
            }
        }

        function formatDuration(ns) {
            if (!ns) return '--';
            const ms = ns / 1000000;
            if (ms < 1000) return ms.toFixed(0) + 'ms';
            const seconds = ms / 1000;
            if (seconds < 60) return seconds.toFixed(1) + 's';
            const minutes = Math.floor(seconds / 60);
            const hours = Math.floor(minutes / 60);
            const days = Math.floor(hours / 24);
            if (days > 0) return days + 'd ' + (hours %% 24) + 'h';
            if (hours > 0) return hours + 'h ' + (minutes %% 60) + 'm';
            return minutes + 'm';
        }

        function getStatusColor(status) {
            const statusLower = status.toLowerCase();
            if (statusLower === 'healthy') return 'green';
            if (statusLower === 'degraded') return 'yellow';
            if (statusLower === 'unhealthy') return 'red';
            return 'gray';
        }
    </script>
</body>
</html>`,
		title,
		title,
		basePath, basePath, basePath,
		func() string {
			if enableRealtime {
				return "initWebSocket();"
			}
			return ""
		}(),
		theme,
		basePath, basePath, basePath, basePath,
		basePath,
		basePath,
		basePath,
	)
}

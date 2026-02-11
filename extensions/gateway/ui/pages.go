package ui

import (
	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui/alpine"
	"github.com/xraph/forgeui/theme"
)

// GatewayDashboardPage renders the complete gateway dashboard page.
func GatewayDashboardPage(title, basePath string, enableRealtime bool) g.Node {
	return html.HTML(
		html.Lang("en"),
		html.Head(
			theme.HeadContent(theme.DefaultLight(), theme.DefaultDark()),
			html.TitleEl(g.Text(title)),
			html.Script(html.Src("https://cdn.tailwindcss.com")),
			theme.TailwindConfigScript(),
			alpine.CloakCSS(),
			theme.StyleTag(theme.DefaultLight(), theme.DefaultDark()),
			html.StyleEl(g.Raw(`
				@layer base {
					* { @apply border-border; }
				}
			`)),
			html.Script(html.Src("https://cdn.jsdelivr.net/npm/apexcharts")),
		),
		html.Body(
			html.Class("min-h-screen bg-background text-foreground antialiased"),
			g.Attr("x-data", "{}"),

			// Initialize Alpine store
			html.Script(g.Raw(gatewayAlpineStore(basePath, enableRealtime))),

			// Dark mode toggle
			theme.DarkModeScript(),

			// Header
			GatewayHeader(title),

			// Main Content
			html.Main(
				html.Class("container py-6 md:py-8"),
				html.Div(
					g.Attr("x-data", `{ activeTab: 'overview' }`),
					html.Class("space-y-6"),

					// Tab Navigation
					html.Div(
						html.Class("border-b"),
						html.Nav(
							html.Class("-mb-px flex space-x-8"),
							tabButton("overview", "Overview"),
							tabButton("routes", "Routes"),
							tabButton("upstreams", "Upstreams"),
							tabButton("services", "Services"),
						),
					),

					// Tab Panels
					OverviewPanel(),
					RoutesPanel(),
					UpstreamsPanel(),
					ServicesPanel(),
				),
			),
		),
	)
}

func tabButton(id, label string) g.Node {
	return html.Button(
		g.Attr("@click", "activeTab = '"+id+"'"),
		g.Attr(":class", "activeTab === '"+id+"' ? 'border-primary text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'"),
		html.Class("inline-flex items-center gap-2 px-1 py-3 border-b-2 text-sm font-medium transition-colors"),
		g.Text(label),
	)
}

func gatewayAlpineStore(basePath string, enableRealtime bool) string {
	wsSetup := ""
	if enableRealtime {
		wsSetup = `
			const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
			const ws = new WebSocket(proto + '//' + location.host + '` + basePath + `/ws');
			ws.onmessage = (e) => {
				try {
					const msg = JSON.parse(e.data);
					if (msg.type === 'stats') Alpine.store('gw').stats = msg.data;
					if (msg.type === 'routes') Alpine.store('gw').routes = msg.data;
				} catch (err) {}
			};
			ws.onclose = () => setTimeout(() => location.reload(), 5000);
		`
	}

	return `
document.addEventListener('alpine:init', () => {
    Alpine.store('gw', {
        stats: null,
        routes: [],
        upstreams: [],
        services: [],
        async fetchAll() {
            try {
                const [statsRes, routesRes, upstreamsRes, servicesRes] = await Promise.all([
                    fetch('` + basePath + `/api/stats'),
                    fetch('` + basePath + `/api/routes'),
                    fetch('` + basePath + `/api/upstreams'),
                    fetch('` + basePath + `/api/discovery/services'),
                ]);
                this.stats = await statsRes.json();
                this.routes = await routesRes.json();
                this.upstreams = await upstreamsRes.json();
                this.services = await servicesRes.json();
            } catch (e) { console.error('Gateway dashboard fetch error', e); }
        },
        formatLatency(ms) {
            if (!ms) return '0ms';
            return ms < 1000 ? ms.toFixed(1) + 'ms' : (ms / 1000).toFixed(2) + 's';
        },
        init() {
            this.fetchAll();
            setInterval(() => this.fetchAll(), 5000);
            ` + wsSetup + `
        }
    });
});
`
}

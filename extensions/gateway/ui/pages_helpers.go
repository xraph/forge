package ui

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

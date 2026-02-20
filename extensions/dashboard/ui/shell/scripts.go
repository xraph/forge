package shell

import (
	"fmt"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"
)

// DashboardStoreScript initializes the Alpine.js store for dashboard state management.
// It provides shared state for sidebar collapse, theme, notifications, and basePath.
func DashboardStoreScript(basePath, nonce string) g.Node {
	js := fmt.Sprintf(`document.addEventListener('alpine:init', () => {
    Alpine.store('dashboard', {
        basePath: '%s',
        sidebarOpen: true,
        sidebarMobileOpen: false,
        connected: false,
        notifications: [],
        unreadCount: 0,

        toggleSidebar() {
            this.sidebarOpen = !this.sidebarOpen;
        },
        toggleMobileSidebar() {
            this.sidebarMobileOpen = !this.sidebarMobileOpen;
        },
        closeMobileSidebar() {
            this.sidebarMobileOpen = false;
        },
        addNotification(n) {
            this.notifications.unshift(n);
            this.unreadCount++;
            if (this.notifications.length > 50) {
                this.notifications.pop();
            }
        },
        clearNotifications() {
            this.notifications = [];
            this.unreadCount = 0;
        },
    });
});`, basePath)

	attrs := []g.Node{g.Attr("type", "text/javascript")}
	if nonce != "" {
		attrs = append(attrs, g.Attr("nonce", nonce))
	}

	return html.Script(append(attrs, g.Raw(js))...)
}

// HTMXConfigScript configures HTMX behavior for the dashboard.
func HTMXConfigScript(nonce string) g.Node {
	js := `document.addEventListener('DOMContentLoaded', function() {
    document.body.setAttribute('hx-ext', 'head-support');

    document.body.addEventListener('htmx:beforeSwap', function(evt) {
        if (evt.detail.xhr.status === 404) {
            evt.detail.shouldSwap = true;
            evt.detail.isError = false;
        }
    });

    document.body.addEventListener('htmx:afterSettle', function(evt) {
        if (window.Alpine) {
            Alpine.nextTick(() => {});
        }
    });
});`

	attrs := []g.Node{g.Attr("type", "text/javascript")}
	if nonce != "" {
		attrs = append(attrs, g.Attr("nonce", nonce))
	}

	return html.Script(append(attrs, g.Raw(js))...)
}

// HelperScripts provides utility JavaScript functions used throughout the dashboard.
func HelperScripts(nonce string) g.Node {
	js := `window.forgeDashboard = {
    formatDuration: function(ms) {
        if (ms < 1000) return ms + 'ms';
        if (ms < 60000) return (ms / 1000).toFixed(1) + 's';
        if (ms < 3600000) return (ms / 60000).toFixed(1) + 'm';
        return (ms / 3600000).toFixed(1) + 'h';
    },
    formatTimestamp: function(ts) {
        if (!ts) return '-';
        var d = new Date(ts);
        return d.toLocaleTimeString();
    },
    formatBytes: function(bytes) {
        if (bytes === 0) return '0 B';
        var k = 1024;
        var sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        var i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    },
    formatNumber: function(n) {
        if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
        if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
        return n.toString();
    },
};`

	attrs := []g.Node{g.Attr("type", "text/javascript")}
	if nonce != "" {
		attrs = append(attrs, g.Attr("nonce", nonce))
	}

	return html.Script(append(attrs, g.Raw(js))...)
}

// AuthRedirectScript handles HTMX 401 responses by reading the HX-Redirect
// header and performing a full-page redirect to the login page.
// This is necessary because HTMX partial requests cannot use normal HTTP
// redirects — the PageMiddleware returns a 401 with HX-Redirect header instead.
func AuthRedirectScript(nonce string) g.Node {
	js := `document.addEventListener('DOMContentLoaded', function() {
    // Handle HTMX response errors (e.g. 401 Unauthorized).
    document.body.addEventListener('htmx:responseError', function(evt) {
        var xhr = evt.detail.xhr;
        if (xhr && xhr.status === 401) {
            var redirect = xhr.getResponseHeader('HX-Redirect');
            if (redirect) {
                window.location.href = redirect;
                return;
            }
        }
    });

    // Also handle htmx:beforeSwap to catch 401s before swap attempts.
    document.body.addEventListener('htmx:beforeSwap', function(evt) {
        var xhr = evt.detail.xhr;
        if (xhr && xhr.status === 401) {
            var redirect = xhr.getResponseHeader('HX-Redirect');
            if (redirect) {
                evt.detail.shouldSwap = false;
                window.location.href = redirect;
                return;
            }
        }
    });
});`

	attrs := []g.Node{g.Attr("type", "text/javascript")}
	if nonce != "" {
		attrs = append(attrs, g.Attr("nonce", nonce))
	}

	return html.Script(append(attrs, g.Raw(js))...)
}

// HTMXScript returns the HTMX CDN script tag.
func HTMXScript() g.Node {
	return html.Script(
		g.Attr("src", "https://unpkg.com/htmx.org@2.0.4"),
		g.Attr("crossorigin", "anonymous"),
	)
}

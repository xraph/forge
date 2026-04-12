package shell

import (
	"fmt"
	"sync"
)

var (
	cachedDashboardStoreJS string
	cachedSSEClientJS      string
	jsCacheMu              sync.Mutex
)

// dashboardStoreJS returns the JavaScript that initializes the Alpine.js store
// for dashboard state management (sidebar collapse, theme, notifications, basePath).
// The result is cached since basePath never changes during the lifetime of the extension.
func dashboardStoreJS(basePath string) string {
	jsCacheMu.Lock()
	defer jsCacheMu.Unlock()

	if cachedDashboardStoreJS != "" {
		return cachedDashboardStoreJS
	}

	cachedDashboardStoreJS = fmt.Sprintf(`document.addEventListener('alpine:init', () => {
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
        markRead() {
            this.unreadCount = 0;
        },
        clearNotifications() {
            this.notifications = [];
            this.unreadCount = 0;
        },
    });
});`, basePath)

	return cachedDashboardStoreJS
}

const htmxConfigJS = `document.addEventListener('DOMContentLoaded', function() {
    document.body.setAttribute('hx-ext', 'head-support');

    // --- Navigation guard ---
    // Exposed globally so SSE refresh can check whether a user navigation is in flight.
    // This prevents SSE-triggered htmx.ajax() from racing with user-initiated navigation,
    // which causes the aborted XHR to fire onerror -> htmx:sendError.
    window.__forgeNavInFlight = false;

    // --- Navigation loading indicator ---
    var _navBar = null;
    function getNavBar() {
        if (!_navBar) {
            _navBar = document.createElement('div');
            _navBar.id = 'forge-nav-bar';
            _navBar.style.cssText = 'position:fixed;top:0;left:0;height:2px;background:hsl(var(--primary,210 100% 50%));z-index:9999;transition:width 0.3s ease;width:0;pointer-events:none;';
            document.body.appendChild(_navBar);
        }
        return _navBar;
    }
    var _navTimer = null;

    document.body.addEventListener('htmx:beforeRequest', function(evt) {
        var target = evt.detail.target;
        if (target && target.id === 'content') {
            window.__forgeNavInFlight = true;
            var bar = getNavBar();
            bar.style.width = '0';
            bar.style.opacity = '1';
            void bar.offsetWidth;
            bar.style.width = '70%';
            clearTimeout(_navTimer);
        }
    });

    document.body.addEventListener('htmx:afterRequest', function(evt) {
        var target = evt.detail.target;
        if (target && target.id === 'content') {
            window.__forgeNavInFlight = false;
            var bar = getNavBar();
            bar.style.width = '100%';
            _navTimer = setTimeout(function() {
                bar.style.opacity = '0';
                setTimeout(function() { bar.style.width = '0'; }, 300);
            }, 150);
        }
    });

    // --- Error handling ---
    document.body.addEventListener('htmx:sendError', function(evt) {
        window.__forgeNavInFlight = false;
        var target = evt.detail.target;
        if (target && target.id === 'content') {
            target.innerHTML = '<div class="flex flex-col items-center justify-center py-16 text-center"><p class="text-lg font-medium text-destructive">Connection Error</p><p class="mt-1 text-sm text-muted-foreground">Failed to load the page. Check your connection and try again.</p><button onclick="htmx.ajax(\'GET\', window.location.pathname, {target:\'#content\', swap:\'innerHTML\'})" class="mt-4 rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90">Retry</button></div>';
        }
        var bar = getNavBar();
        bar.style.background = 'hsl(var(--destructive,0 84% 60%))';
        bar.style.width = '100%';
        setTimeout(function() {
            bar.style.opacity = '0';
            setTimeout(function() {
                bar.style.width = '0';
                bar.style.background = 'hsl(var(--primary,210 100% 50%))';
            }, 300);
        }, 1000);
    });

    document.body.addEventListener('htmx:responseError', function(evt) {
        var xhr = evt.detail.xhr;
        var target = evt.detail.target;
        if (target && target.id === 'content' && xhr && xhr.status >= 500) {
            target.innerHTML = '<div class="flex flex-col items-center justify-center py-16 text-center"><p class="text-lg font-medium text-destructive">Server Error (' + xhr.status + ')</p><p class="mt-1 text-sm text-muted-foreground">Something went wrong. Please try again.</p><button onclick="htmx.ajax(\'GET\', window.location.pathname, {target:\'#content\', swap:\'innerHTML\'})" class="mt-4 rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90">Retry</button></div>';
        }
    });

    // --- Existing handlers ---
    document.body.addEventListener('htmx:beforeSwap', function(evt) {
        if (evt.detail.xhr.status === 404) {
            evt.detail.shouldSwap = true;
            evt.detail.isError = false;
        }
    });

    document.body.addEventListener('htmx:afterSettle', function(evt) {
        if (window.Alpine && evt.detail && evt.detail.elt) {
            Alpine.initTree(evt.detail.elt);
        }
    });
});`

const helperScriptsJS = `window.forgeDashboard = {
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

// sseClientJS returns JavaScript that establishes an SSE connection
// and forwards events to the Alpine.js store.
// The result is cached since basePath never changes during the lifetime of the extension.
func sseClientJS(basePath string) string {
	jsCacheMu.Lock()
	defer jsCacheMu.Unlock()

	if cachedSSEClientJS != "" {
		return cachedSSEClientJS
	}

	cachedSSEClientJS = fmt.Sprintf(`document.addEventListener('DOMContentLoaded', function() {
    var evtSource = null;
    var reconnectTimer = null;

    // Shared across SSE reconnections — keeps debounce state stable.
    var _sseTimers = {};
    var _pageLoadTime = Date.now();

    // Debounced SSE page refresh — re-fetches current page content via HTMX
    // only when the user is viewing the relevant page and no navigation is in flight.
    function ssePageRefresh(pageKey) {
        // Guard: skip refreshes within first 2s to let Alpine.js initialise.
        if (Date.now() - _pageLoadTime < 2000) return;
        // Guard: skip if a user-initiated HTMX navigation is in progress.
        // Firing htmx.ajax while another #content request is in flight causes the
        // older XHR to be aborted, which triggers onerror -> htmx:sendError.
        if (window.__forgeNavInFlight) return;
        clearTimeout(_sseTimers[pageKey]);
        _sseTimers[pageKey] = setTimeout(function() {
            // Re-check guard inside the timeout — navigation may have started since the event.
            if (window.__forgeNavInFlight) return;
            var content = document.getElementById('content');
            if (!content || !window.htmx) return;
            var el = document.querySelector('[data-sse-refresh="' + pageKey + '"]');
            if (!el) return;
            htmx.ajax('GET', window.location.pathname, {target: '#content', swap: 'innerHTML'});
        }, 500);
    }

    function connect() {
        if (evtSource) {
            evtSource.close();
        }

        evtSource = new EventSource('%s/sse');

        evtSource.addEventListener('connected', function() {
            if (window.Alpine && Alpine.store('dashboard')) {
                Alpine.store('dashboard').connected = true;
            }
        });

        evtSource.addEventListener('notification', function(e) {
            try {
                var data = JSON.parse(e.data);
                if (window.Alpine && Alpine.store('dashboard')) {
                    Alpine.store('dashboard').addNotification(data);
                }
            } catch (err) {
                console.warn('Failed to parse notification:', err);
            }
        });

        evtSource.addEventListener('health-update', function(e) {
            try {
                var data = JSON.parse(e.data);
                if (window.Alpine && Alpine.store('dashboard')) {
                    Alpine.store('dashboard').lastHealth = data;
                }
                ssePageRefresh('health');
            } catch (err) {
                console.warn('Failed to parse health-update:', err);
            }
        });

        evtSource.addEventListener('metrics-update', function(e) {
            try {
                var data = JSON.parse(e.data);
                if (window.Alpine && Alpine.store('dashboard')) {
                    Alpine.store('dashboard').lastMetrics = data;
                }
                ssePageRefresh('metrics');
            } catch (err) {
                console.warn('Failed to parse metrics-update:', err);
            }
        });

        evtSource.addEventListener('trace-update', function(e) {
            try {
                var data = JSON.parse(e.data);
                if (window.Alpine && Alpine.store('dashboard')) {
                    Alpine.store('dashboard').lastTrace = data;
                }
                ssePageRefresh('traces');
            } catch (err) {
                console.warn('Failed to parse trace-update:', err);
            }
        });

        evtSource.onerror = function() {
            if (window.Alpine && Alpine.store('dashboard')) {
                Alpine.store('dashboard').connected = false;
            }
            evtSource.close();
            clearTimeout(reconnectTimer);
            reconnectTimer = setTimeout(connect, 5000);
        };
    }

    connect();

    window.addEventListener('beforeunload', function() {
        if (evtSource) {
            evtSource.close();
        }
    });
});`, basePath)

	return cachedSSEClientJS
}

const searchKeyboardJS = `document.addEventListener('keydown', function(e) {
    if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        if (window.tui && window.tui.dialog) {
            window.tui.dialog.toggle('forge-search');
        }
    }
});`

const authRedirectJS = `document.addEventListener('DOMContentLoaded', function() {
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

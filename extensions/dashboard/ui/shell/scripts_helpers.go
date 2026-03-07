package shell

import "fmt"

// dashboardStoreJS returns the JavaScript that initializes the Alpine.js store
// for dashboard state management (sidebar collapse, theme, notifications, basePath).
func dashboardStoreJS(basePath string) string {
	return fmt.Sprintf(`document.addEventListener('alpine:init', () => {
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
}

const htmxConfigJS = `document.addEventListener('DOMContentLoaded', function() {
    document.body.setAttribute('hx-ext', 'head-support');

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
func sseClientJS(basePath string) string {
	return fmt.Sprintf(`document.addEventListener('DOMContentLoaded', function() {
    var evtSource = null;
    var reconnectTimer = null;

    // Shared across SSE reconnections — keeps debounce state stable.
    var _sseTimers = {};
    var _pageLoadTime = Date.now();

    // Debounced SSE page refresh — re-fetches current page content via HTMX
    // only when the user is viewing the relevant page.
    function ssePageRefresh(pageKey) {
        // Guard: skip refreshes within first 2s to let Alpine.js initialise.
        if (Date.now() - _pageLoadTime < 2000) return;
        clearTimeout(_sseTimers[pageKey]);
        _sseTimers[pageKey] = setTimeout(function() {
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

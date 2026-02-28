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
        if (window.Alpine) {
            Alpine.nextTick(() => {});
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

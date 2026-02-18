package contributor

// Manifest describes a contributor's capabilities, navigation, widgets, settings, and more.
type Manifest struct {
	Name            string               `json:"name"`
	DisplayName     string               `json:"display_name"`
	Icon            string               `json:"icon"`
	Version         string               `json:"version"`
	Layout          string               `json:"layout,omitempty"` // "dashboard", "settings", "base", "full" — default "dashboard"
	Nav             []NavItem            `json:"nav"`
	Widgets         []WidgetDescriptor   `json:"widgets"`
	Settings        []SettingsDescriptor `json:"settings"`
	SearchProviders []SearchProviderDef  `json:"search_providers,omitempty"`
	Notifications   []NotificationDef    `json:"notifications,omitempty"`
	Capabilities    []string             `json:"capabilities,omitempty"`
}

// NavItem represents a navigation entry in the dashboard sidebar.
type NavItem struct {
	Label    string    `json:"label"`
	Path     string    `json:"path"`
	Icon     string    `json:"icon"`
	Badge    string    `json:"badge,omitempty"`
	Group    string    `json:"group,omitempty"`
	Children []NavItem `json:"children,omitempty"`
	Priority int       `json:"priority,omitempty"`
}

// WidgetDescriptor describes a widget that can be rendered on the dashboard.
type WidgetDescriptor struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Size        string `json:"size"`        // "sm", "md", "lg"
	RefreshSec  int    `json:"refresh_sec"` // 0 = static
	Group       string `json:"group"`
	Priority    int    `json:"priority"`
}

// SettingsDescriptor describes a settings panel contributed by an extension.
type SettingsDescriptor struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Group       string `json:"group"`
	Icon        string `json:"icon"`
	Priority    int    `json:"priority"`
}

// SearchProviderDef describes a search provider contributed by an extension.
type SearchProviderDef struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Prefix      string   `json:"prefix,omitempty"`       // e.g., "user:" filters to this provider
	ResultTypes []string `json:"result_types,omitempty"` // "page", "entity", "action"
}

// NotificationDef describes a notification type an extension can emit.
type NotificationDef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Severity string `json:"severity"` // "info", "warning", "error", "critical"
}

// SearchResult represents a single search result from a contributor.
type SearchResult struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	URL         string  `json:"url"`
	Icon        string  `json:"icon"`
	Source      string  `json:"source"` // contributor name
	Category    string  `json:"category"`
	Score       float64 `json:"score"`
}

// Notification represents a real-time notification from a contributor.
type Notification struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
	URL      string `json:"url,omitempty"`
	Time     int64  `json:"time"` // unix timestamp
}

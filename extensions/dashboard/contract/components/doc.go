// Package components provides typed, fluent builders for authoring dashboard
// contract graphs from Go. Every builder emits a contract.GraphNode whose
// shape matches what the React shell (extensions/dashboard/contract/shell)
// expects on the wire — Go contributors get compile-time safety; YAML
// authors keep using the loose map[string]any form.
//
// # Tiers
//
// The catalog is organized into four tiers, following atomic-design:
//
//   - layout.*    structural containers: Row, Column, Grid, Stack,
//     Container, Section, Card, Tabs, Accordion, Split,
//     Divider, Spacer, Page.
//   - atom.*      leaf controls: Button, Input, Badge, Label, Text,
//     Heading, Link, Icon, Avatar, Image, Separator, Skeleton,
//     Spinner, Progress, Checkbox, Radio, Switch, Slider,
//     Select, Textarea, Kbd, Code, Tooltip.
//   - molecule.*  composed controls: Field, StatCard, Alert, SearchBar,
//     EmptyState, Breadcrumb, Pagination, TagInput,
//     FileUpload, DatePicker, Combobox, CommandPalette,
//     DropdownMenu, ListItem, Chip, Rating.
//   - organism.*  composite views: DataGrid, DynamicForm, Kanban,
//     Calendar, Timeline, TreeView, Stepper, Chart,
//     CodeEditor, NotificationCenter, Comments.
//
// # Usage
//
//	import comp "github.com/xraph/forge/extensions/dashboard/contract/components"
//
//	page := comp.Page("Members").
//	    Description("Manage team membership").
//	    Children(
//	        comp.Grid().Cols(3).Gap("4").Children(
//	            comp.StatCard("Total members").Value("128").Trend(comp.TrendUp).Build(),
//	            comp.StatCard("Active sessions").Value("45").Build(),
//	            comp.StatCard("Pending invites").Value("3").Build(),
//	        ).Build(),
//	        comp.Card().Title("Roster").Content(
//	            comp.DataGrid().
//	                Data(comp.Query("members.list")).
//	                Columns(memberCols).
//	                Selection(comp.SelectionMulti()).
//	                Pagination(comp.ServerPagination().PageSize(25)).
//	                Build(),
//	        ).Build(),
//	    ).Build()
//
// # Wire format
//
// All builders Build() into a contract.GraphNode. Typed props are JSON-
// serialized into GraphNode.Props (map[string]any) so downstream code —
// merger, registry, transport, React renderer — operates on the same wire
// shape regardless of how a node was authored.
//
// # Color tokens
//
// Style enums (ColorVariant, Size, Spacing, etc.) emit only canonical
// shadcn semantic tokens — no success / warning / info, no custom brand
// colors. The React shell's intent renderers translate these tokens into
// Tailwind utilities at render time.
package components

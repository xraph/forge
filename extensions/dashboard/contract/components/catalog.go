package components

// Catalog of every intent name this package exposes. Keep in lockstep with
// the React shell's intents/register.ts — the registry_test.go file checks
// that every builder constructed in Go produces a node whose Intent string
// matches one of these constants, which protects against typos and silent
// renames.

// Layout intents.
const (
	IntentLayoutPage      = "layout.page"
	IntentLayoutRow       = "layout.row"
	IntentLayoutColumn    = "layout.column"
	IntentLayoutGrid      = "layout.grid"
	IntentLayoutStack     = "layout.stack"
	IntentLayoutContainer = "layout.container"
	IntentLayoutSection   = "layout.section"
	IntentLayoutCard      = "layout.card"
	IntentLayoutTabs      = "layout.tabs"
	IntentLayoutAccordion = "layout.accordion"
	IntentLayoutSplit     = "layout.split"
	IntentLayoutDivider   = "layout.divider"
	IntentLayoutSpacer    = "layout.spacer"
)

// Atom intents.
const (
	IntentAtomButton     = "atom.button"
	IntentAtomIconButton = "atom.icon-button"
	IntentAtomBadge      = "atom.badge"
	IntentAtomLabel      = "atom.label"
	IntentAtomText       = "atom.text"
	IntentAtomHeading    = "atom.heading"
	IntentAtomLink       = "atom.link"
	IntentAtomIcon       = "atom.icon"
	IntentAtomAvatar     = "atom.avatar"
	IntentAtomImage      = "atom.image"
	IntentAtomSeparator  = "atom.separator"
	IntentAtomSkeleton   = "atom.skeleton"
	IntentAtomSpinner    = "atom.spinner"
	IntentAtomProgress   = "atom.progress"
	IntentAtomInput      = "atom.input"
	IntentAtomTextarea   = "atom.textarea"
	IntentAtomCheckbox   = "atom.checkbox"
	IntentAtomRadio      = "atom.radio"
	IntentAtomSwitch     = "atom.switch"
	IntentAtomSlider     = "atom.slider"
	IntentAtomSelect     = "atom.select"
	IntentAtomKbd        = "atom.kbd"
	IntentAtomCode       = "atom.code"
	IntentAtomTooltip    = "atom.tooltip"
)

// Molecule intents.
const (
	IntentMoleculeField          = "molecule.field"
	IntentMoleculeStatCard       = "molecule.stat-card"
	IntentMoleculeAlert          = "molecule.alert"
	IntentMoleculeSearchBar      = "molecule.search-bar"
	IntentMoleculeEmptyState     = "molecule.empty-state"
	IntentMoleculeErrorState     = "molecule.error-state"
	IntentMoleculeLoadingState   = "molecule.loading-state"
	IntentMoleculeBreadcrumb     = "molecule.breadcrumb"
	IntentMoleculePagination     = "molecule.pagination"
	IntentMoleculeTagInput       = "molecule.tag-input"
	IntentMoleculeFileUpload     = "molecule.file-upload"
	IntentMoleculeDatePicker     = "molecule.date-picker"
	IntentMoleculeCombobox       = "molecule.combobox"
	IntentMoleculeCommandPalette = "molecule.command-palette"
	IntentMoleculeDropdownMenu   = "molecule.dropdown-menu"
	IntentMoleculeListItem       = "molecule.list-item"
	IntentMoleculeChip           = "molecule.chip"
	IntentMoleculeRating         = "molecule.rating"
)

// Organism intents.
const (
	IntentOrganismDataGrid           = "organism.data-grid"
	IntentOrganismDynamicForm        = "organism.dynamic-form"
	IntentOrganismKanban             = "organism.kanban"
	IntentOrganismCalendar           = "organism.calendar"
	IntentOrganismTimeline           = "organism.timeline"
	IntentOrganismTreeView           = "organism.tree-view"
	IntentOrganismStepper            = "organism.stepper"
	IntentOrganismChart              = "organism.chart"
	IntentOrganismCodeEditor         = "organism.code-editor"
	IntentOrganismNotificationCenter = "organism.notification-center"
	IntentOrganismComments           = "organism.comments"
)

// Authsome composite intents.
const (
	IntentAuthSignInForm     = "auth.signin-form"
	IntentAuthSignUpForm     = "auth.signup-form"
	IntentAuthTabs           = "auth.tabs"
	IntentAuthMagicLinkForm  = "auth.magic-link-form"
	IntentAuthOAuthButtons   = "auth.oauth-buttons"
	IntentAuthUserButton     = "auth.user-button"
	IntentAuthAccountMenu    = "auth.account-menu"
	IntentAuthOrgSwitcher    = "auth.org-switcher"
	IntentAuthOrgProfile     = "auth.org-profile"
	IntentAuthTwoFactorSetup = "auth.two-factor-setup"
	IntentAuthPasskeyPrompt  = "auth.passkey-prompt"
	IntentAuthSessionList    = "auth.session-list"
)

// Intents returns the full set of intent names this package emits. Useful
// for contract loaders or audit tooling that need to validate a contributor's
// manifest references only known intents.
func Intents() []string {
	return []string{
		// Layout
		IntentLayoutPage, IntentLayoutRow, IntentLayoutColumn, IntentLayoutGrid,
		IntentLayoutStack, IntentLayoutContainer, IntentLayoutSection, IntentLayoutCard,
		IntentLayoutTabs, IntentLayoutAccordion, IntentLayoutSplit, IntentLayoutDivider,
		IntentLayoutSpacer,
		// Atoms
		IntentAtomButton, IntentAtomIconButton, IntentAtomBadge, IntentAtomLabel,
		IntentAtomText, IntentAtomHeading, IntentAtomLink, IntentAtomIcon,
		IntentAtomAvatar, IntentAtomImage, IntentAtomSeparator, IntentAtomSkeleton,
		IntentAtomSpinner, IntentAtomProgress, IntentAtomInput, IntentAtomTextarea,
		IntentAtomCheckbox, IntentAtomRadio, IntentAtomSwitch, IntentAtomSlider,
		IntentAtomSelect, IntentAtomKbd, IntentAtomCode, IntentAtomTooltip,
		// Molecules
		IntentMoleculeField, IntentMoleculeStatCard, IntentMoleculeAlert,
		IntentMoleculeSearchBar, IntentMoleculeEmptyState, IntentMoleculeErrorState,
		IntentMoleculeLoadingState, IntentMoleculeBreadcrumb, IntentMoleculePagination,
		IntentMoleculeTagInput, IntentMoleculeFileUpload, IntentMoleculeDatePicker,
		IntentMoleculeCombobox, IntentMoleculeCommandPalette, IntentMoleculeDropdownMenu,
		IntentMoleculeListItem, IntentMoleculeChip, IntentMoleculeRating,
		// Organisms
		IntentOrganismDataGrid, IntentOrganismDynamicForm, IntentOrganismKanban,
		IntentOrganismCalendar, IntentOrganismTimeline, IntentOrganismTreeView,
		IntentOrganismStepper, IntentOrganismChart, IntentOrganismCodeEditor,
		IntentOrganismNotificationCenter, IntentOrganismComments,
		// Auth
		IntentAuthSignInForm, IntentAuthSignUpForm, IntentAuthTabs,
		IntentAuthMagicLinkForm, IntentAuthOAuthButtons, IntentAuthUserButton,
		IntentAuthAccountMenu, IntentAuthOrgSwitcher, IntentAuthOrgProfile,
		IntentAuthTwoFactorSetup, IntentAuthPasskeyPrompt, IntentAuthSessionList,
	}
}

// IntentSet returns the catalog as a lookup map keyed by intent name.
// O(1) membership checks for validators.
func IntentSet() map[string]struct{} {
	out := make(map[string]struct{}, len(Intents()))
	for _, n := range Intents() {
		out[n] = struct{}{}
	}
	return out
}

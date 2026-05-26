import { IntentRegistry, type IntentComponent } from "../runtime/registry";

// Existing intents (kept for backward compatibility).
import { PageShell } from "./page.shell";
import { MetricCounter } from "./metric.counter";
import { ActionButton } from "./action.button";
import { ActionMenu } from "./action.menu";
import { ActionDivider } from "./action.divider";
import { FormEdit } from "./form.edit";
import { FormField } from "./form.field";
import { ResourceList } from "./resource.list";
import { ResourceDetail } from "./resource.detail";
import { DashboardGrid } from "./dashboard.grid";
import { AuditTail } from "./audit.tail";
import { AuthLoginForm } from "../auth/AuthLoginForm";

// Layout intents (Phase 1).
import { Row } from "./layout.row";
import { Column } from "./layout.column";
import { Grid } from "./layout.grid";
import { Stack } from "./layout.stack";
import { Container } from "./layout.container";
import { Section } from "./layout.section";
import { Card } from "./layout.card";
import { TabsLayout } from "./layout.tabs";
import { AccordionLayout } from "./layout.accordion";
import { Split } from "./layout.split";
import { Divider } from "./layout.divider";
import { Spacer } from "./layout.spacer";
import { Page } from "./layout.page";

// Atom intents (Phase 1).
import { AtomButton } from "./atom.button";
import { IconButton as AtomIconButton } from "./atom.icon-button";
import { AtomBadge } from "./atom.badge";
import { AtomLabel } from "./atom.label";
import { AtomText } from "./atom.text";
import { AtomHeading } from "./atom.heading";
import { AtomLink } from "./atom.link";
import { Icon as AtomIcon } from "./atom.icon";
import { AtomAvatar } from "./atom.avatar";
import { AtomImage } from "./atom.image";
import { AtomSeparator } from "./atom.separator";
import { AtomSkeleton } from "./atom.skeleton";
import { AtomSpinner } from "./atom.spinner";
import { AtomProgress } from "./atom.progress";
import { AtomInput } from "./atom.input";
import { AtomTextarea } from "./atom.textarea";
import { AtomCheckbox } from "./atom.checkbox";
import { AtomRadio } from "./atom.radio";
import { AtomSwitch } from "./atom.switch";
import { AtomSlider } from "./atom.slider";
import { AtomSelect } from "./atom.select";
import { AtomKbd } from "./atom.kbd";
import { AtomCode } from "./atom.code";
import { AtomTooltip } from "./atom.tooltip";

// Molecule intents (Phase 2).
import { MoleculeField } from "./molecule.field";
import { StatCard } from "./molecule.stat-card";
import { MoleculeAlert } from "./molecule.alert";
import { SearchBar } from "./molecule.search-bar";
import { EmptyState } from "./molecule.empty-state";
import { ErrorState } from "./molecule.error-state";
import { LoadingState } from "./molecule.loading-state";
import { MoleculeBreadcrumb } from "./molecule.breadcrumb";
import { MoleculePagination } from "./molecule.pagination";
import { TagInput } from "./molecule.tag-input";
import { FileUpload } from "./molecule.file-upload";
import { DatePicker } from "./molecule.date-picker";
import { Combobox } from "./molecule.combobox";
import { CommandPalette } from "./molecule.command-palette";
import { MoleculeDropdownMenu } from "./molecule.dropdown-menu";
import { ListItem } from "./molecule.list-item";
import { Chip } from "./molecule.chip";
import { Rating } from "./molecule.rating";

// Organisms (Phase 2 — DynamicForm).
import { DynamicForm } from "./organism.dynamic-form";

// Organisms (Phase 3).
import { DataGrid } from "./organism.data-grid";
import { Kanban } from "./organism.kanban";
import { Calendar } from "./organism.calendar";
import { Timeline } from "./organism.timeline";
import { TreeView } from "./organism.tree-view";
import { Stepper } from "./organism.stepper";
import { Chart } from "./organism.chart";
import { CodeEditor } from "./organism.code-editor";
import { NotificationCenter } from "./organism.notification-center";
import { Comments } from "./organism.comments";

// Authsome composites (Phase 4).
import { SignInForm as AuthSignInForm } from "./auth.signin-form";
import { SignUpForm as AuthSignUpForm } from "./auth.signup-form";
import { AuthTabs } from "./auth.tabs";
import { MagicLinkForm as AuthMagicLinkForm } from "./auth.magic-link-form";
import { OAuthButtons as AuthOAuthButtons } from "./auth.oauth-buttons";
import { UserButton as AuthUserButton } from "./auth.user-button";
import { AccountMenu as AuthAccountMenu } from "./auth.account-menu";
import { OrgSwitcher as AuthOrgSwitcher } from "./auth.org-switcher";
import { OrgProfile as AuthOrgProfile } from "./auth.org-profile";
import { TwoFactorSetup as AuthTwoFactorSetup } from "./auth.two-factor-setup";
import { PasskeyPrompt as AuthPasskeyPrompt } from "./auth.passkey-prompt";
import { SessionList as AuthSessionList } from "./auth.session-list";

// Authsome migration additions (Phase 5).
import { ForgotPasswordForm as AuthForgotPasswordForm } from "./auth.forgot-password-form";
import { ResetPasswordForm as AuthResetPasswordForm } from "./auth.reset-password-form";
import { SetupForm as AuthSetupForm } from "./auth.setup-form";
import { DynamicSignupForm as AuthDynamicSignupForm } from "./auth.dynamic-signup-form";
import { DetailHeader } from "./detail.header";
import { DetailSection } from "./detail.section";
import { DashboardStat } from "./dashboard.stat";
import { DashboardRecentList } from "./dashboard.recentlist";
import { EditorCode } from "./editor.code";
import { EditorFormBuilder } from "./editor.formBuilder";
import { MetricGauge } from "./metric.gauge";
import { ConfirmDialog } from "./confirm.dialog";
import { FilterSearch } from "./filter.search";
import { FilterSelect } from "./filter.select";
import { FilterDate } from "./filter.date";
import { PaginationCursor } from "./pagination.cursor";
import { PaginationOffset } from "./pagination.offset";
import { FieldJson } from "./field.json";
import { FieldPermissions } from "./field.permissions";
import { SettingsTabs } from "./settings.tabs";
import { SettingsPanel } from "./settings.panel";

export function buildIntentRegistry(): IntentRegistry {
  const reg = new IntentRegistry();

  // Existing intents — unchanged.
  reg.register("page.shell", PageShell as unknown as IntentComponent);
  reg.register("metric.counter", MetricCounter as unknown as IntentComponent);
  reg.register("action.button", ActionButton as unknown as IntentComponent);
  reg.register("action.menu", ActionMenu as unknown as IntentComponent);
  reg.register("action.divider", ActionDivider as unknown as IntentComponent);
  reg.register("form.edit", FormEdit as unknown as IntentComponent);
  reg.register("form.field", FormField as unknown as IntentComponent);
  reg.register("resource.list", ResourceList as unknown as IntentComponent);
  reg.register("resource.detail", ResourceDetail as unknown as IntentComponent);
  reg.register("dashboard.grid", DashboardGrid as unknown as IntentComponent);
  reg.register("audit.tail", AuditTail as unknown as IntentComponent);
  reg.register("auth.login.form", AuthLoginForm as unknown as IntentComponent);

  // Layout intents.
  reg.register("layout.page", Page as unknown as IntentComponent);
  reg.register("layout.row", Row as unknown as IntentComponent);
  reg.register("layout.column", Column as unknown as IntentComponent);
  reg.register("layout.grid", Grid as unknown as IntentComponent);
  reg.register("layout.stack", Stack as unknown as IntentComponent);
  reg.register("layout.container", Container as unknown as IntentComponent);
  reg.register("layout.section", Section as unknown as IntentComponent);
  reg.register("layout.card", Card as unknown as IntentComponent);
  reg.register("layout.tabs", TabsLayout as unknown as IntentComponent);
  reg.register("layout.accordion", AccordionLayout as unknown as IntentComponent);
  reg.register("layout.split", Split as unknown as IntentComponent);
  reg.register("layout.divider", Divider as unknown as IntentComponent);
  reg.register("layout.spacer", Spacer as unknown as IntentComponent);

  // Atom intents.
  reg.register("atom.button", AtomButton as unknown as IntentComponent);
  reg.register("atom.icon-button", AtomIconButton as unknown as IntentComponent);
  reg.register("atom.badge", AtomBadge as unknown as IntentComponent);
  reg.register("atom.label", AtomLabel as unknown as IntentComponent);
  reg.register("atom.text", AtomText as unknown as IntentComponent);
  reg.register("atom.heading", AtomHeading as unknown as IntentComponent);
  reg.register("atom.link", AtomLink as unknown as IntentComponent);
  reg.register("atom.icon", AtomIcon as unknown as IntentComponent);
  reg.register("atom.avatar", AtomAvatar as unknown as IntentComponent);
  reg.register("atom.image", AtomImage as unknown as IntentComponent);
  reg.register("atom.separator", AtomSeparator as unknown as IntentComponent);
  reg.register("atom.skeleton", AtomSkeleton as unknown as IntentComponent);
  reg.register("atom.spinner", AtomSpinner as unknown as IntentComponent);
  reg.register("atom.progress", AtomProgress as unknown as IntentComponent);
  reg.register("atom.input", AtomInput as unknown as IntentComponent);
  reg.register("atom.textarea", AtomTextarea as unknown as IntentComponent);
  reg.register("atom.checkbox", AtomCheckbox as unknown as IntentComponent);
  reg.register("atom.radio", AtomRadio as unknown as IntentComponent);
  reg.register("atom.switch", AtomSwitch as unknown as IntentComponent);
  reg.register("atom.slider", AtomSlider as unknown as IntentComponent);
  reg.register("atom.select", AtomSelect as unknown as IntentComponent);
  reg.register("atom.kbd", AtomKbd as unknown as IntentComponent);
  reg.register("atom.code", AtomCode as unknown as IntentComponent);
  reg.register("atom.tooltip", AtomTooltip as unknown as IntentComponent);

  // Molecule intents.
  reg.register("molecule.field", MoleculeField as unknown as IntentComponent);
  reg.register("molecule.stat-card", StatCard as unknown as IntentComponent);
  reg.register("molecule.alert", MoleculeAlert as unknown as IntentComponent);
  reg.register("molecule.search-bar", SearchBar as unknown as IntentComponent);
  reg.register("molecule.empty-state", EmptyState as unknown as IntentComponent);
  reg.register("molecule.error-state", ErrorState as unknown as IntentComponent);
  reg.register("molecule.loading-state", LoadingState as unknown as IntentComponent);
  reg.register("molecule.breadcrumb", MoleculeBreadcrumb as unknown as IntentComponent);
  reg.register("molecule.pagination", MoleculePagination as unknown as IntentComponent);
  reg.register("molecule.tag-input", TagInput as unknown as IntentComponent);
  reg.register("molecule.file-upload", FileUpload as unknown as IntentComponent);
  reg.register("molecule.date-picker", DatePicker as unknown as IntentComponent);
  reg.register("molecule.combobox", Combobox as unknown as IntentComponent);
  reg.register("molecule.command-palette", CommandPalette as unknown as IntentComponent);
  reg.register("molecule.dropdown-menu", MoleculeDropdownMenu as unknown as IntentComponent);
  reg.register("molecule.list-item", ListItem as unknown as IntentComponent);
  reg.register("molecule.chip", Chip as unknown as IntentComponent);
  reg.register("molecule.rating", Rating as unknown as IntentComponent);

  // Organism intents.
  reg.register("organism.dynamic-form", DynamicForm as unknown as IntentComponent);
  reg.register("organism.data-grid", DataGrid as unknown as IntentComponent);
  reg.register("organism.kanban", Kanban as unknown as IntentComponent);
  reg.register("organism.calendar", Calendar as unknown as IntentComponent);
  reg.register("organism.timeline", Timeline as unknown as IntentComponent);
  reg.register("organism.tree-view", TreeView as unknown as IntentComponent);
  reg.register("organism.stepper", Stepper as unknown as IntentComponent);
  reg.register("organism.chart", Chart as unknown as IntentComponent);
  reg.register("organism.code-editor", CodeEditor as unknown as IntentComponent);
  reg.register("organism.notification-center", NotificationCenter as unknown as IntentComponent);
  reg.register("organism.comments", Comments as unknown as IntentComponent);

  // Authsome composites.
  reg.register("auth.signin-form", AuthSignInForm as unknown as IntentComponent);
  reg.register("auth.signup-form", AuthSignUpForm as unknown as IntentComponent);
  reg.register("auth.tabs", AuthTabs as unknown as IntentComponent);
  reg.register("auth.magic-link-form", AuthMagicLinkForm as unknown as IntentComponent);
  reg.register("auth.oauth-buttons", AuthOAuthButtons as unknown as IntentComponent);
  reg.register("auth.user-button", AuthUserButton as unknown as IntentComponent);
  reg.register("auth.account-menu", AuthAccountMenu as unknown as IntentComponent);
  reg.register("auth.org-switcher", AuthOrgSwitcher as unknown as IntentComponent);
  reg.register("auth.org-profile", AuthOrgProfile as unknown as IntentComponent);
  reg.register("auth.two-factor-setup", AuthTwoFactorSetup as unknown as IntentComponent);
  reg.register("auth.passkey-prompt", AuthPasskeyPrompt as unknown as IntentComponent);
  reg.register("auth.session-list", AuthSessionList as unknown as IntentComponent);

  // Authsome migration additions — auth pages, detail composition,
  // dashboard widgets, editors, confirmation, filters, pagination,
  // form field extensions.
  reg.register("auth.forgot-password-form", AuthForgotPasswordForm as unknown as IntentComponent);
  reg.register("auth.reset-password-form", AuthResetPasswordForm as unknown as IntentComponent);
  reg.register("auth.setup-form", AuthSetupForm as unknown as IntentComponent);
  reg.register("auth.dynamic-signup-form", AuthDynamicSignupForm as unknown as IntentComponent);
  reg.register("detail.header", DetailHeader as unknown as IntentComponent);
  reg.register("detail.section", DetailSection as unknown as IntentComponent);
  reg.register("dashboard.stat", DashboardStat as unknown as IntentComponent);
  reg.register("dashboard.recentlist", DashboardRecentList as unknown as IntentComponent);
  reg.register("editor.code", EditorCode as unknown as IntentComponent);
  reg.register("editor.formBuilder", EditorFormBuilder as unknown as IntentComponent);
  reg.register("metric.gauge", MetricGauge as unknown as IntentComponent);
  reg.register("confirm.dialog", ConfirmDialog as unknown as IntentComponent);
  reg.register("filter.search", FilterSearch as unknown as IntentComponent);
  reg.register("filter.select", FilterSelect as unknown as IntentComponent);
  reg.register("filter.date", FilterDate as unknown as IntentComponent);
  reg.register("pagination.cursor", PaginationCursor as unknown as IntentComponent);
  reg.register("pagination.offset", PaginationOffset as unknown as IntentComponent);
  reg.register("field.json", FieldJson as unknown as IntentComponent);
  reg.register("field.permissions", FieldPermissions as unknown as IntentComponent);

  // Settings UI composites — auto-discover grouping over the
  // settings.* contract surface.
  reg.register("settings.tabs", SettingsTabs as unknown as IntentComponent);
  reg.register("settings.panel", SettingsPanel as unknown as IntentComponent);

  return reg;
}

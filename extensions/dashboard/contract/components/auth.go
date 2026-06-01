package components

import "github.com/xraph/forge/extensions/dashboard/contract"

// =============================================================================
// auth.signin-form — extends auth.login.form with social providers,
// remember-me, magic-link toggle. The renderer pulls per-deployment
// configuration from the `auth.config` intent on the contributor that
// owns the route.
// =============================================================================

type SignInFormProps struct {
	Op                string  `json:"op,omitempty"` // submit command (default: auth.login)
	ConfigContributor string  `json:"configContributor,omitempty"`
	ShowRememberMe    bool    `json:"showRememberMe,omitempty"`
	ShowMagicLink     bool    `json:"showMagicLink,omitempty"`
	ShowSignUpLink    bool    `json:"showSignUpLink,omitempty"`
	SignUpURL         string  `json:"signUpURL,omitempty"`
	ForgotPasswordURL string  `json:"forgotPasswordURL,omitempty"`
	Brand             string  `json:"brand,omitempty"`
	BrandLogoURL      string  `json:"brandLogoURL,omitempty"`
	OnSuccess         *Action `json:"onSuccess,omitempty"`
	ClassName         string  `json:"className,omitempty"`
}

type SignInFormBuilder struct {
	*Common[SignInFormBuilder]
	Props SignInFormProps
}

func SignInForm() *SignInFormBuilder {
	b := &SignInFormBuilder{}
	b.Common = newCommon[SignInFormBuilder]("auth.signin-form", b)
	b.Props.ShowSignUpLink = true
	return b
}

func (b *SignInFormBuilder) OpName(o string) *SignInFormBuilder { b.Props.Op = o; return b }
func (b *SignInFormBuilder) ConfigContributor(c string) *SignInFormBuilder {
	b.Props.ConfigContributor = c
	return b
}
func (b *SignInFormBuilder) ShowRememberMe(s bool) *SignInFormBuilder {
	b.Props.ShowRememberMe = s
	return b
}
func (b *SignInFormBuilder) ShowMagicLink(s bool) *SignInFormBuilder {
	b.Props.ShowMagicLink = s
	return b
}
func (b *SignInFormBuilder) ShowSignUpLink(s bool) *SignInFormBuilder {
	b.Props.ShowSignUpLink = s
	return b
}
func (b *SignInFormBuilder) SignUpURL(u string) *SignInFormBuilder { b.Props.SignUpURL = u; return b }
func (b *SignInFormBuilder) ForgotPasswordURL(u string) *SignInFormBuilder {
	b.Props.ForgotPasswordURL = u
	return b
}
func (b *SignInFormBuilder) Brand(name string) *SignInFormBuilder { b.Props.Brand = name; return b }
func (b *SignInFormBuilder) BrandLogoURL(u string) *SignInFormBuilder {
	b.Props.BrandLogoURL = u
	return b
}
func (b *SignInFormBuilder) OnSuccess(a *Action) *SignInFormBuilder { b.Props.OnSuccess = a; return b }
func (b *SignInFormBuilder) ClassName(c string) *SignInFormBuilder  { b.Props.ClassName = c; return b }
func (b *SignInFormBuilder) Build() contract.GraphNode              { return b.toNode(b.Props) }

// =============================================================================
// auth.signup-form
// =============================================================================

type SignUpFormProps struct {
	Op             string   `json:"op"`
	ShowOAuth      bool     `json:"showOAuth,omitempty"`
	OAuthProviders []string `json:"oauthProviders,omitempty"`
	NameField      bool     `json:"nameField,omitempty"`
	TermsURL       string   `json:"termsURL,omitempty"`
	PrivacyURL     string   `json:"privacyURL,omitempty"`
	SignInURL      string   `json:"signInURL,omitempty"`
	Brand          string   `json:"brand,omitempty"`
	OnSuccess      *Action  `json:"onSuccess,omitempty"`
	ClassName      string   `json:"className,omitempty"`
}

type SignUpFormBuilder struct {
	*Common[SignUpFormBuilder]
	Props SignUpFormProps
}

func SignUpForm() *SignUpFormBuilder {
	b := &SignUpFormBuilder{}
	b.Common = newCommon[SignUpFormBuilder]("auth.signup-form", b)
	b.Props.NameField = true
	return b
}

func (b *SignUpFormBuilder) OpName(o string) *SignUpFormBuilder  { b.Props.Op = o; return b }
func (b *SignUpFormBuilder) ShowOAuth(s bool) *SignUpFormBuilder { b.Props.ShowOAuth = s; return b }
func (b *SignUpFormBuilder) OAuthProviders(p ...string) *SignUpFormBuilder {
	b.Props.OAuthProviders = p
	return b
}
func (b *SignUpFormBuilder) NameField(n bool) *SignUpFormBuilder    { b.Props.NameField = n; return b }
func (b *SignUpFormBuilder) TermsURL(u string) *SignUpFormBuilder   { b.Props.TermsURL = u; return b }
func (b *SignUpFormBuilder) PrivacyURL(u string) *SignUpFormBuilder { b.Props.PrivacyURL = u; return b }
func (b *SignUpFormBuilder) SignInURL(u string) *SignUpFormBuilder  { b.Props.SignInURL = u; return b }
func (b *SignUpFormBuilder) Brand(name string) *SignUpFormBuilder   { b.Props.Brand = name; return b }
func (b *SignUpFormBuilder) OnSuccess(a *Action) *SignUpFormBuilder { b.Props.OnSuccess = a; return b }
func (b *SignUpFormBuilder) ClassName(c string) *SignUpFormBuilder  { b.Props.ClassName = c; return b }
func (b *SignUpFormBuilder) Build() contract.GraphNode              { return b.toNode(b.Props) }

// =============================================================================
// auth.tabs
// =============================================================================

type AuthTabsProps struct {
	DefaultTab     string   `json:"defaultTab,omitempty"` // "signin" | "signup"
	SignInOp       string   `json:"signInOp,omitempty"`
	SignUpOp       string   `json:"signUpOp,omitempty"`
	OAuthProviders []string `json:"oauthProviders,omitempty"`
	Brand          string   `json:"brand,omitempty"`
	ClassName      string   `json:"className,omitempty"`
}

type AuthTabsBuilder struct {
	*Common[AuthTabsBuilder]
	Props AuthTabsProps
}

func AuthTabs() *AuthTabsBuilder {
	b := &AuthTabsBuilder{}
	b.Common = newCommon[AuthTabsBuilder]("auth.tabs", b)
	b.Props.DefaultTab = "signin"
	return b
}

func (b *AuthTabsBuilder) DefaultTab(t string) *AuthTabsBuilder { b.Props.DefaultTab = t; return b }
func (b *AuthTabsBuilder) SignInOp(o string) *AuthTabsBuilder   { b.Props.SignInOp = o; return b }
func (b *AuthTabsBuilder) SignUpOp(o string) *AuthTabsBuilder   { b.Props.SignUpOp = o; return b }
func (b *AuthTabsBuilder) OAuthProviders(p ...string) *AuthTabsBuilder {
	b.Props.OAuthProviders = p
	return b
}
func (b *AuthTabsBuilder) Brand(name string) *AuthTabsBuilder  { b.Props.Brand = name; return b }
func (b *AuthTabsBuilder) ClassName(c string) *AuthTabsBuilder { b.Props.ClassName = c; return b }
func (b *AuthTabsBuilder) Build() contract.GraphNode           { return b.toNode(b.Props) }

// =============================================================================
// auth.magic-link-form
// =============================================================================

type MagicLinkFormProps struct {
	Op             string  `json:"op"`
	Brand          string  `json:"brand,omitempty"`
	SuccessMessage string  `json:"successMessage,omitempty"`
	BackToSignIn   string  `json:"backToSignIn,omitempty"`
	OnSuccess      *Action `json:"onSuccess,omitempty"`
	ClassName      string  `json:"className,omitempty"`
}

type MagicLinkFormBuilder struct {
	*Common[MagicLinkFormBuilder]
	Props MagicLinkFormProps
}

func MagicLinkForm() *MagicLinkFormBuilder {
	b := &MagicLinkFormBuilder{}
	b.Common = newCommon[MagicLinkFormBuilder]("auth.magic-link-form", b)
	return b
}

func (b *MagicLinkFormBuilder) OpName(o string) *MagicLinkFormBuilder { b.Props.Op = o; return b }
func (b *MagicLinkFormBuilder) Brand(name string) *MagicLinkFormBuilder {
	b.Props.Brand = name
	return b
}
func (b *MagicLinkFormBuilder) SuccessMessage(m string) *MagicLinkFormBuilder {
	b.Props.SuccessMessage = m
	return b
}
func (b *MagicLinkFormBuilder) BackToSignIn(u string) *MagicLinkFormBuilder {
	b.Props.BackToSignIn = u
	return b
}
func (b *MagicLinkFormBuilder) OnSuccess(a *Action) *MagicLinkFormBuilder {
	b.Props.OnSuccess = a
	return b
}
func (b *MagicLinkFormBuilder) ClassName(c string) *MagicLinkFormBuilder {
	b.Props.ClassName = c
	return b
}
func (b *MagicLinkFormBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// auth.oauth-buttons
// =============================================================================

type OAuthProvider struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
	// AuthStartURL is the absolute URL the shell POSTs to begin the OAuth
	// flow. The endpoint responds with `{auth_url}` which the shell then
	// navigates to.
	AuthStartURL string `json:"authStartURL"`
}

type OAuthButtonsProps struct {
	Providers    []OAuthProvider       `json:"providers,omitempty"`
	ConfigSource *contract.DataBinding `json:"configSource,omitempty"` // dynamic providers
	Variant      ColorVariant          `json:"variant,omitempty"`
	FullWidth    bool                  `json:"fullWidth,omitempty"`
	Layout       string                `json:"layout,omitempty"` // "grid" | "stack"
	RedirectURL  string                `json:"redirectURL,omitempty"`
	ClassName    string                `json:"className,omitempty"`
}

type OAuthButtonsBuilder struct {
	*Common[OAuthButtonsBuilder]
	Props OAuthButtonsProps
}

func OAuthButtons() *OAuthButtonsBuilder {
	b := &OAuthButtonsBuilder{}
	b.Common = newCommon[OAuthButtonsBuilder]("auth.oauth-buttons", b)
	b.Props.Variant = VariantOutline
	b.Props.FullWidth = true
	b.Props.Layout = "stack"
	return b
}

func (b *OAuthButtonsBuilder) Providers(p ...OAuthProvider) *OAuthButtonsBuilder {
	b.Props.Providers = append(b.Props.Providers, p...)
	return b
}
func (b *OAuthButtonsBuilder) ConfigSource(d *contract.DataBinding) *OAuthButtonsBuilder {
	b.Props.ConfigSource = d
	return b
}
func (b *OAuthButtonsBuilder) Variant(v ColorVariant) *OAuthButtonsBuilder {
	b.Props.Variant = v
	return b
}
func (b *OAuthButtonsBuilder) FullWidth(w bool) *OAuthButtonsBuilder { b.Props.FullWidth = w; return b }
func (b *OAuthButtonsBuilder) Layout(l string) *OAuthButtonsBuilder  { b.Props.Layout = l; return b }
func (b *OAuthButtonsBuilder) RedirectURL(u string) *OAuthButtonsBuilder {
	b.Props.RedirectURL = u
	return b
}
func (b *OAuthButtonsBuilder) ClassName(c string) *OAuthButtonsBuilder {
	b.Props.ClassName = c
	return b
}
func (b *OAuthButtonsBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// auth.user-button — avatar trigger with account dropdown
// =============================================================================

type UserButtonProps struct {
	ShowName    bool       `json:"showName,omitempty"`
	ShowEmail   bool       `json:"showEmail,omitempty"`
	MenuItems   []MenuItem `json:"menuItems,omitempty"`
	SignOutOp   string     `json:"signOutOp,omitempty"`
	ProfileURL  string     `json:"profileURL,omitempty"`
	SettingsURL string     `json:"settingsURL,omitempty"`
	ClassName   string     `json:"className,omitempty"`
}

type UserButtonBuilder struct {
	*Common[UserButtonBuilder]
	Props UserButtonProps
}

func UserButton() *UserButtonBuilder {
	b := &UserButtonBuilder{}
	b.Common = newCommon[UserButtonBuilder]("auth.user-button", b)
	return b
}

func (b *UserButtonBuilder) ShowName(s bool) *UserButtonBuilder  { b.Props.ShowName = s; return b }
func (b *UserButtonBuilder) ShowEmail(s bool) *UserButtonBuilder { b.Props.ShowEmail = s; return b }
func (b *UserButtonBuilder) MenuItem(item MenuItem) *UserButtonBuilder {
	b.Props.MenuItems = append(b.Props.MenuItems, item)
	return b
}
func (b *UserButtonBuilder) SignOutOp(op string) *UserButtonBuilder { b.Props.SignOutOp = op; return b }
func (b *UserButtonBuilder) ProfileURL(u string) *UserButtonBuilder { b.Props.ProfileURL = u; return b }
func (b *UserButtonBuilder) SettingsURL(u string) *UserButtonBuilder {
	b.Props.SettingsURL = u
	return b
}
func (b *UserButtonBuilder) ClassName(c string) *UserButtonBuilder { b.Props.ClassName = c; return b }
func (b *UserButtonBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// auth.account-menu — full inline account panel
// =============================================================================

type AccountMenuProps struct {
	SignOutOp       string `json:"signOutOp,omitempty"`
	ProfileURL      string `json:"profileURL,omitempty"`
	SettingsURL     string `json:"settingsURL,omitempty"`
	BillingURL      string `json:"billingURL,omitempty"`
	ShowOrgSwitcher bool   `json:"showOrgSwitcher,omitempty"`
	ClassName       string `json:"className,omitempty"`
}

type AccountMenuBuilder struct {
	*Common[AccountMenuBuilder]
	Props AccountMenuProps
}

func AccountMenu() *AccountMenuBuilder {
	b := &AccountMenuBuilder{}
	b.Common = newCommon[AccountMenuBuilder]("auth.account-menu", b)
	return b
}

func (b *AccountMenuBuilder) SignOutOp(op string) *AccountMenuBuilder {
	b.Props.SignOutOp = op
	return b
}
func (b *AccountMenuBuilder) ProfileURL(u string) *AccountMenuBuilder {
	b.Props.ProfileURL = u
	return b
}
func (b *AccountMenuBuilder) SettingsURL(u string) *AccountMenuBuilder {
	b.Props.SettingsURL = u
	return b
}
func (b *AccountMenuBuilder) BillingURL(u string) *AccountMenuBuilder {
	b.Props.BillingURL = u
	return b
}
func (b *AccountMenuBuilder) ShowOrgSwitcher(s bool) *AccountMenuBuilder {
	b.Props.ShowOrgSwitcher = s
	return b
}
func (b *AccountMenuBuilder) ClassName(c string) *AccountMenuBuilder { b.Props.ClassName = c; return b }
func (b *AccountMenuBuilder) Build() contract.GraphNode              { return b.toNode(b.Props) }

// =============================================================================
// auth.org-switcher
// =============================================================================

type OrgSwitcherProps struct {
	OrgsSource   *contract.DataBinding `json:"orgsSource,omitempty"`
	CurrentField string                `json:"currentField,omitempty"` // path in session to current org
	OnSwitch     *Action               `json:"onSwitch,omitempty"`
	CreateOrgURL string                `json:"createOrgURL,omitempty"`
	ManageOrgURL string                `json:"manageOrgURL,omitempty"`
	ShowPersonal bool                  `json:"showPersonal,omitempty"`
	ClassName    string                `json:"className,omitempty"`
}

type OrgSwitcherBuilder struct {
	*Common[OrgSwitcherBuilder]
	Props OrgSwitcherProps
}

func OrgSwitcher() *OrgSwitcherBuilder {
	b := &OrgSwitcherBuilder{}
	b.Common = newCommon[OrgSwitcherBuilder]("auth.org-switcher", b)
	b.Props.CurrentField = "session.user.organizationID"
	return b
}

func (b *OrgSwitcherBuilder) OrgsSource(d *contract.DataBinding) *OrgSwitcherBuilder {
	b.Props.OrgsSource = d
	return b
}
func (b *OrgSwitcherBuilder) CurrentField(f string) *OrgSwitcherBuilder {
	b.Props.CurrentField = f
	return b
}
func (b *OrgSwitcherBuilder) OnSwitch(a *Action) *OrgSwitcherBuilder { b.Props.OnSwitch = a; return b }
func (b *OrgSwitcherBuilder) CreateOrgURL(u string) *OrgSwitcherBuilder {
	b.Props.CreateOrgURL = u
	return b
}
func (b *OrgSwitcherBuilder) ManageOrgURL(u string) *OrgSwitcherBuilder {
	b.Props.ManageOrgURL = u
	return b
}
func (b *OrgSwitcherBuilder) ShowPersonal(s bool) *OrgSwitcherBuilder {
	b.Props.ShowPersonal = s
	return b
}
func (b *OrgSwitcherBuilder) ClassName(c string) *OrgSwitcherBuilder { b.Props.ClassName = c; return b }
func (b *OrgSwitcherBuilder) Build() contract.GraphNode              { return b.toNode(b.Props) }

// =============================================================================
// auth.org-profile — full-page org settings (tabs: general, members, settings)
// =============================================================================

type OrgProfileProps struct {
	OrgSource          *contract.DataBinding `json:"orgSource,omitempty"`
	MembersSource      *contract.DataBinding `json:"membersSource,omitempty"`
	UpdateOp           string                `json:"updateOp,omitempty"`
	InviteOp           string                `json:"inviteOp,omitempty"`
	RemoveMemberOp     string                `json:"removeMemberOp,omitempty"`
	UpdateMemberRoleOp string                `json:"updateMemberRoleOp,omitempty"`
	DeleteOrgOp        string                `json:"deleteOrgOp,omitempty"`
	AvailableRoles     []Option              `json:"availableRoles,omitempty"`
	ClassName          string                `json:"className,omitempty"`
}

type OrgProfileBuilder struct {
	*Common[OrgProfileBuilder]
	Props OrgProfileProps
}

func OrgProfile() *OrgProfileBuilder {
	b := &OrgProfileBuilder{}
	b.Common = newCommon[OrgProfileBuilder]("auth.org-profile", b)
	return b
}

func (b *OrgProfileBuilder) OrgSource(d *contract.DataBinding) *OrgProfileBuilder {
	b.Props.OrgSource = d
	return b
}
func (b *OrgProfileBuilder) MembersSource(d *contract.DataBinding) *OrgProfileBuilder {
	b.Props.MembersSource = d
	return b
}
func (b *OrgProfileBuilder) UpdateOp(op string) *OrgProfileBuilder { b.Props.UpdateOp = op; return b }
func (b *OrgProfileBuilder) InviteOp(op string) *OrgProfileBuilder { b.Props.InviteOp = op; return b }
func (b *OrgProfileBuilder) RemoveMemberOp(op string) *OrgProfileBuilder {
	b.Props.RemoveMemberOp = op
	return b
}
func (b *OrgProfileBuilder) UpdateMemberRoleOp(op string) *OrgProfileBuilder {
	b.Props.UpdateMemberRoleOp = op
	return b
}
func (b *OrgProfileBuilder) DeleteOrgOp(op string) *OrgProfileBuilder {
	b.Props.DeleteOrgOp = op
	return b
}
func (b *OrgProfileBuilder) AvailableRoles(roles ...Option) *OrgProfileBuilder {
	b.Props.AvailableRoles = append(b.Props.AvailableRoles, roles...)
	return b
}
func (b *OrgProfileBuilder) ClassName(c string) *OrgProfileBuilder { b.Props.ClassName = c; return b }
func (b *OrgProfileBuilder) Build() contract.GraphNode             { return b.toNode(b.Props) }

// =============================================================================
// auth.two-factor-setup — multi-step wizard for enrolling TOTP/SMS/etc.
// =============================================================================

type TwoFactorSetupProps struct {
	GenerateSecretOp string  `json:"generateSecretOp"`
	VerifyOp         string  `json:"verifyOp"`
	GenerateBackupOp string  `json:"generateBackupOp,omitempty"`
	Method           string  `json:"method,omitempty"` // "totp" | "sms"
	OnComplete       *Action `json:"onComplete,omitempty"`
	ClassName        string  `json:"className,omitempty"`
}

type TwoFactorSetupBuilder struct {
	*Common[TwoFactorSetupBuilder]
	Props TwoFactorSetupProps
}

func TwoFactorSetup() *TwoFactorSetupBuilder {
	b := &TwoFactorSetupBuilder{}
	b.Common = newCommon[TwoFactorSetupBuilder]("auth.two-factor-setup", b)
	b.Props.Method = "totp"
	return b
}

func (b *TwoFactorSetupBuilder) GenerateSecretOp(op string) *TwoFactorSetupBuilder {
	b.Props.GenerateSecretOp = op
	return b
}
func (b *TwoFactorSetupBuilder) VerifyOp(op string) *TwoFactorSetupBuilder {
	b.Props.VerifyOp = op
	return b
}
func (b *TwoFactorSetupBuilder) GenerateBackupOp(op string) *TwoFactorSetupBuilder {
	b.Props.GenerateBackupOp = op
	return b
}
func (b *TwoFactorSetupBuilder) Method(m string) *TwoFactorSetupBuilder { b.Props.Method = m; return b }
func (b *TwoFactorSetupBuilder) OnComplete(a *Action) *TwoFactorSetupBuilder {
	b.Props.OnComplete = a
	return b
}
func (b *TwoFactorSetupBuilder) ClassName(c string) *TwoFactorSetupBuilder {
	b.Props.ClassName = c
	return b
}
func (b *TwoFactorSetupBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// auth.passkey-prompt
// =============================================================================

type PasskeyPromptProps struct {
	RegisterOp  string  `json:"registerOp"`
	OnComplete  *Action `json:"onComplete,omitempty"`
	OnSkip      *Action `json:"onSkip,omitempty"`
	SkipLabel   string  `json:"skipLabel,omitempty"`
	Dismissible bool    `json:"dismissible,omitempty"`
	ClassName   string  `json:"className,omitempty"`
}

type PasskeyPromptBuilder struct {
	*Common[PasskeyPromptBuilder]
	Props PasskeyPromptProps
}

func PasskeyPrompt() *PasskeyPromptBuilder {
	b := &PasskeyPromptBuilder{}
	b.Common = newCommon[PasskeyPromptBuilder]("auth.passkey-prompt", b)
	b.Props.Dismissible = true
	b.Props.SkipLabel = "Skip for now"
	return b
}

func (b *PasskeyPromptBuilder) RegisterOp(op string) *PasskeyPromptBuilder {
	b.Props.RegisterOp = op
	return b
}
func (b *PasskeyPromptBuilder) OnComplete(a *Action) *PasskeyPromptBuilder {
	b.Props.OnComplete = a
	return b
}
func (b *PasskeyPromptBuilder) OnSkip(a *Action) *PasskeyPromptBuilder { b.Props.OnSkip = a; return b }
func (b *PasskeyPromptBuilder) SkipLabel(l string) *PasskeyPromptBuilder {
	b.Props.SkipLabel = l
	return b
}
func (b *PasskeyPromptBuilder) Dismissible(d bool) *PasskeyPromptBuilder {
	b.Props.Dismissible = d
	return b
}
func (b *PasskeyPromptBuilder) ClassName(c string) *PasskeyPromptBuilder {
	b.Props.ClassName = c
	return b
}
func (b *PasskeyPromptBuilder) Build() contract.GraphNode { return b.toNode(b.Props) }

// =============================================================================
// auth.session-list — active sessions table with revoke action
// =============================================================================

type SessionListProps struct {
	RevokeOp            string `json:"revokeOp,omitempty"`
	RevokeAllOp         string `json:"revokeAllOp,omitempty"`
	ShowCurrent         bool   `json:"showCurrent,omitempty"`
	CurrentSessionField string `json:"currentSessionField,omitempty"`
	ClassName           string `json:"className,omitempty"`
}

type SessionListBuilder struct {
	*Common[SessionListBuilder]
	Props SessionListProps
}

func SessionList() *SessionListBuilder {
	b := &SessionListBuilder{}
	b.Common = newCommon[SessionListBuilder]("auth.session-list", b)
	b.Props.ShowCurrent = true
	return b
}

func (b *SessionListBuilder) RevokeOp(op string) *SessionListBuilder { b.Props.RevokeOp = op; return b }
func (b *SessionListBuilder) RevokeAllOp(op string) *SessionListBuilder {
	b.Props.RevokeAllOp = op
	return b
}
func (b *SessionListBuilder) ShowCurrent(s bool) *SessionListBuilder {
	b.Props.ShowCurrent = s
	return b
}
func (b *SessionListBuilder) CurrentSessionField(f string) *SessionListBuilder {
	b.Props.CurrentSessionField = f
	return b
}
func (b *SessionListBuilder) ClassName(c string) *SessionListBuilder { b.Props.ClassName = c; return b }
func (b *SessionListBuilder) Build() contract.GraphNode              { return b.toNode(b.Props) }

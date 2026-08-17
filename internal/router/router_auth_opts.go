package router

// WithAuth adds authentication to a route using one or more providers.
// Multiple providers create an OR condition - any one succeeding allows access.
//
// Example:
//
//	router.GET("/protected", handler,
//	    forge.WithAuth("api-key", "jwt"),
//	)
func WithAuth(providerNames ...string) RouteOption {
	return &routeAuth{
		providers: providerNames,
		mode:      "or",
	}
}

// WithRequiredAuth adds authentication with required scopes/permissions.
// The specified provider must succeed AND the auth context must have all required scopes.
//
// Example:
//
//	router.POST("/admin/users", handler,
//	    forge.WithRequiredAuth("jwt", "write:users", "admin"),
//	)
func WithRequiredAuth(providerName string, scopes ...string) RouteOption {
	return &routeAuth{
		providers: []string{providerName},
		scopes:    scopes,
		mode:      "or",
	}
}

// WithAuthAnd requires ALL specified providers to succeed (AND condition).
// This is useful for multi-factor authentication or combining auth methods.
//
// Example:
//
//	router.GET("/high-security", handler,
//	    forge.WithAuthAnd("api-key", "mfa"),
//	)
func WithAuthAnd(providerNames ...string) RouteOption {
	return &routeAuth{
		providers: providerNames,
		mode:      "and",
	}
}

type routeAuth struct {
	providers []string
	scopes    []string
	mode      string // "or" or "and"
}

func (o *routeAuth) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]any)
	}

	config.Metadata["auth.providers"] = o.providers
	config.Metadata["auth.scopes"] = o.scopes
	config.Metadata["auth.mode"] = o.mode
}

// WithGroupAuth adds authentication to all routes in a group.
// Multiple providers create an OR condition.
//
// Example:
//
//	api := router.Group("/api", forge.WithGroupAuth("jwt"))
func WithGroupAuth(providerNames ...string) GroupOption {
	return &groupAuth{
		providers: providerNames,
		mode:      "or",
	}
}

// WithGroupAuthAnd requires all providers to succeed for all routes in the group.
func WithGroupAuthAnd(providerNames ...string) GroupOption {
	return &groupAuth{
		providers: providerNames,
		mode:      "and",
	}
}

// WithGroupRequiredScopes sets required scopes for all routes in a group.
func WithGroupRequiredScopes(scopes ...string) GroupOption {
	return &groupScopes{
		scopes: scopes,
	}
}

type groupAuth struct {
	providers []string
	mode      string
}

func (o *groupAuth) Apply(config *GroupConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]any)
	}

	config.Metadata["auth.providers"] = o.providers
	config.Metadata["auth.mode"] = o.mode
}

type groupScopes struct {
	scopes []string
}

func (o *groupScopes) Apply(config *GroupConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]any)
	}

	config.Metadata["auth.scopes"] = o.scopes
}

// WithAnyRole requires the authenticated subject to hold at least one of the
// given roles.
//
// The name says "any" because a name that hides whether it means and or or
// sends you to the source to find out. Use WithAllPermissions for all-of
// semantics.
//
// Example:
//
//	router.POST("/admin/users", handler,
//	    forge.WithRequiredAuth("jwt"),
//	    forge.WithAnyRole("admin", "editor"),
//	)
func WithAnyRole(roles ...string) RouteOption {
	return &routeRoles{roles: roles}
}

type routeRoles struct {
	roles []string
}

func (o *routeRoles) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]any)
	}

	config.Metadata["auth.roles"] = o.roles
}

// WithAllPermissions requires the authenticated subject to hold every one of
// the given permissions.
//
// Example:
//
//	router.DELETE("/users/:id", handler,
//	    forge.WithRequiredAuth("jwt"),
//	    forge.WithAllPermissions("users:write", "users:delete"),
//	)
func WithAllPermissions(permissions ...string) RouteOption {
	return &routePermissions{permissions: permissions}
}

type routePermissions struct {
	permissions []string
}

func (o *routePermissions) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]any)
	}

	config.Metadata["auth.permissions"] = o.permissions
}

// WithGroupAnyRole applies WithAnyRole to every route in a group.
func WithGroupAnyRole(roles ...string) GroupOption {
	return &groupRoles{roles: roles}
}

type groupRoles struct {
	roles []string
}

func (o *groupRoles) Apply(config *GroupConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]any)
	}

	config.Metadata["auth.roles"] = o.roles
}

// WithGroupAllPermissions applies WithAllPermissions to every route in a group.
func WithGroupAllPermissions(permissions ...string) GroupOption {
	return &groupPermissions{permissions: permissions}
}

type groupPermissions struct {
	permissions []string
}

func (o *groupPermissions) Apply(config *GroupConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]any)
	}

	config.Metadata["auth.permissions"] = o.permissions
}

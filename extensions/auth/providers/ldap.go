package providers

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/auth"
)

// LDAPConfig holds LDAP/Active Directory configuration
type LDAPConfig struct {
	// Connection settings
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`

	// Bind credentials (service account)
	BindDN       string `yaml:"bind_dn" json:"bind_dn"`
	BindPassword string `yaml:"bind_password" json:"bind_password"`

	// Search settings
	BaseDN       string   `yaml:"base_dn" json:"base_dn"`
	SearchFilter string   `yaml:"search_filter" json:"search_filter"` // e.g., "(uid=%s)" or "(sAMAccountName=%s)"
	Attributes   []string `yaml:"attributes" json:"attributes"`       // Attributes to fetch

	// TLS settings
	UseTLS             bool `yaml:"use_tls" json:"use_tls"`
	InsecureSkipVerify bool `yaml:"insecure_skip_verify" json:"insecure_skip_verify"`

	// Connection pool
	PoolSize          int           `yaml:"pool_size" json:"pool_size"`
	ConnectionTimeout time.Duration `yaml:"connection_timeout" json:"connection_timeout"`
	RequestTimeout    time.Duration `yaml:"request_timeout" json:"request_timeout"`
	IdleTimeout       time.Duration `yaml:"idle_timeout" json:"idle_timeout"`
	MaxRetries        int           `yaml:"max_retries" json:"max_retries"`
	RetryDelay        time.Duration `yaml:"retry_delay" json:"retry_delay"`

	// Cache settings
	EnableCache bool          `yaml:"enable_cache" json:"enable_cache"`
	CacheTTL    time.Duration `yaml:"cache_ttl" json:"cache_ttl"`

	// Group mapping
	GroupBaseDN string            `yaml:"group_base_dn" json:"group_base_dn"` // e.g., "ou=groups,dc=company,dc=com"
	GroupFilter string            `yaml:"group_filter" json:"group_filter"`   // e.g., "(member=%s)"
	RoleMapping map[string]string `yaml:"role_mapping" json:"role_mapping"`   // LDAP group DN -> app role

	// Advanced
	EnableReferrals bool `yaml:"enable_referrals" json:"enable_referrals"` // Handle AD referrals
	PageSize        int  `yaml:"page_size" json:"page_size"`               // Paging for large result sets
}

// DefaultLDAPConfig returns default LDAP configuration
func DefaultLDAPConfig() LDAPConfig {
	return LDAPConfig{
		Port:               389,
		UseTLS:             true,
		InsecureSkipVerify: false,
		PoolSize:           10,
		ConnectionTimeout:  10 * time.Second,
		RequestTimeout:     30 * time.Second,
		IdleTimeout:        5 * time.Minute,
		MaxRetries:         3,
		RetryDelay:         100 * time.Millisecond,
		EnableCache:        true,
		CacheTTL:           5 * time.Minute,
		SearchFilter:       "(uid=%s)",
		Attributes:         []string{"uid", "mail", "displayName", "memberOf"},
		GroupFilter:        "(member=%s)",
		EnableReferrals:    true,
		PageSize:           1000,
	}
}

// LDAPProvider implements LDAP/Active Directory authentication
type LDAPProvider struct {
	config   LDAPConfig
	connPool *ldapConnPool
	cache    *ldapCache
	logger   forge.Logger
	mu       sync.RWMutex
}

// ldapConnPool manages a pool of LDAP connections (binding is expensive!)
type ldapConnPool struct {
	conns   chan *ldap.Conn
	factory func() (*ldap.Conn, error)
	config  LDAPConfig
	logger  forge.Logger
	mu      sync.Mutex
	closed  bool
}

// ldapCache caches authentication results
type ldapCache struct {
	entries map[string]*cacheEntry
	mu      sync.RWMutex
	ttl     time.Duration
}

type cacheEntry struct {
	authCtx   *auth.AuthContext
	expiresAt time.Time
}

// NewLDAPProvider creates a new LDAP authentication provider
func NewLDAPProvider(config LDAPConfig, logger forge.Logger) (*LDAPProvider, error) {
	if config.Host == "" {
		return nil, fmt.Errorf("ldap host is required")
	}
	if config.BaseDN == "" {
		return nil, fmt.Errorf("ldap base_dn is required")
	}
	if config.BindDN == "" {
		return nil, fmt.Errorf("ldap bind_dn is required")
	}

	// Apply defaults
	if config.PoolSize == 0 {
		config.PoolSize = 10
	}
	if config.ConnectionTimeout == 0 {
		config.ConnectionTimeout = 10 * time.Second
	}
	if config.RequestTimeout == 0 {
		config.RequestTimeout = 30 * time.Second
	}
	if config.CacheTTL == 0 {
		config.CacheTTL = 5 * time.Minute
	}

	// Create connection pool
	pool := &ldapConnPool{
		conns:  make(chan *ldap.Conn, config.PoolSize),
		config: config,
		logger: logger,
	}

	pool.factory = func() (*ldap.Conn, error) {
		return pool.createConnection()
	}

	// Pre-populate pool
	for i := 0; i < config.PoolSize; i++ {
		conn, err := pool.factory()
		if err != nil {
			logger.Warn("failed to pre-populate connection pool",
				forge.F("error", err),
				forge.F("attempt", i+1),
			)
			continue
		}
		pool.conns <- conn
	}

	// Create cache
	var cache *ldapCache
	if config.EnableCache {
		cache = &ldapCache{
			entries: make(map[string]*cacheEntry),
			ttl:     config.CacheTTL,
		}
		// Start cache cleanup goroutine
		go cache.cleanup()
	}

	provider := &LDAPProvider{
		config:   config,
		connPool: pool,
		cache:    cache,
		logger:   logger,
	}

	logger.Info("ldap provider initialized",
		forge.F("host", config.Host),
		forge.F("port", config.Port),
		forge.F("base_dn", config.BaseDN),
		forge.F("pool_size", config.PoolSize),
	)

	return provider, nil
}

// Name returns the provider name
func (p *LDAPProvider) Name() string {
	return "ldap"
}

// Type returns the security scheme type
func (p *LDAPProvider) Type() auth.SecuritySchemeType {
	return auth.SecurityTypeHTTP
}

// Authenticate authenticates a user against LDAP/AD
func (p *LDAPProvider) Authenticate(ctx context.Context, r *http.Request) (*auth.AuthContext, error) {
	// Extract Basic Auth credentials
	username, password, ok := r.BasicAuth()
	if !ok || username == "" || password == "" {
		return nil, fmt.Errorf("missing or invalid basic auth credentials")
	}

	// Check cache first (if enabled)
	if p.config.EnableCache && p.cache != nil {
		if authCtx := p.cache.get(username, password); authCtx != nil {
			p.logger.Debug("ldap auth cache hit", forge.F("username", username))
			return authCtx, nil
		}
	}

	// Authenticate with LDAP
	authCtx, err := p.authenticateLDAP(ctx, username, password)
	if err != nil {
		p.logger.Warn("ldap authentication failed",
			forge.F("username", username),
			forge.F("error", err),
		)
		return nil, err
	}

	// Cache the result (if enabled)
	if p.config.EnableCache && p.cache != nil {
		p.cache.set(username, password, authCtx)
	}

	p.logger.Info("ldap authentication successful",
		forge.F("username", username),
		forge.F("groups", authCtx.Metadata["groups"]),
	)

	return authCtx, nil
}

// authenticateLDAP performs the actual LDAP authentication
func (p *LDAPProvider) authenticateLDAP(ctx context.Context, username, password string) (*auth.AuthContext, error) {
	// Get connection from pool with timeout
	conn, err := p.connPool.getConn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ldap connection: %w", err)
	}
	defer p.connPool.putConn(conn)

	// Bind as service account to search for user DN
	if err := conn.Bind(p.config.BindDN, p.config.BindPassword); err != nil {
		return nil, fmt.Errorf("service account bind failed: %w", err)
	}

	// Search for user DN
	searchFilter := fmt.Sprintf(p.config.SearchFilter, ldap.EscapeFilter(username))
	searchRequest := ldap.NewSearchRequest(
		p.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0, 0, false,
		searchFilter,
		p.config.Attributes,
		nil,
	)

	sr, err := conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("user search failed: %w", err)
	}

	if len(sr.Entries) == 0 {
		return nil, fmt.Errorf("user not found: %s", username)
	}

	if len(sr.Entries) > 1 {
		return nil, fmt.Errorf("multiple users found for username: %s", username)
	}

	userEntry := sr.Entries[0]
	userDN := userEntry.DN

	// Attempt to bind as the user (validates password)
	if err := conn.Bind(userDN, password); err != nil {
		return nil, fmt.Errorf("authentication failed for user %s: %w", username, err)
	}

	// Extract user attributes
	claims := make(map[string]interface{})
	for _, attr := range p.config.Attributes {
		value := userEntry.GetAttributeValue(attr)
		if value != "" {
			claims[attr] = value
		}
	}

	// Fetch group memberships
	groups, err := p.fetchGroups(ctx, conn, userDN)
	if err != nil {
		p.logger.Warn("failed to fetch user groups",
			forge.F("username", username),
			forge.F("error", err),
		)
		groups = []string{}
	}

	// Map LDAP groups to application roles
	roles := p.mapGroupsToRoles(groups)

	authCtx := &auth.AuthContext{
		Subject:      username,
		Claims:       claims,
		Scopes:       roles,
		ProviderName: "ldap",
		Metadata: map[string]interface{}{
			"dn":     userDN,
			"groups": groups,
		},
	}

	return authCtx, nil
}

// fetchGroups fetches group memberships for a user
func (p *LDAPProvider) fetchGroups(ctx context.Context, conn *ldap.Conn, userDN string) ([]string, error) {
	if p.config.GroupBaseDN == "" {
		return []string{}, nil
	}

	// Re-bind as service account for group search
	if err := conn.Bind(p.config.BindDN, p.config.BindPassword); err != nil {
		return nil, fmt.Errorf("service account bind for group search failed: %w", err)
	}

	// Search for groups where user is a member
	searchFilter := fmt.Sprintf(p.config.GroupFilter, ldap.EscapeFilter(userDN))
	searchRequest := ldap.NewSearchRequest(
		p.config.GroupBaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0, 0, false,
		searchFilter,
		[]string{"cn", "dn"},
		nil,
	)

	sr, err := conn.Search(searchRequest)
	if err != nil {
		return nil, err
	}

	groups := make([]string, 0, len(sr.Entries))
	for _, entry := range sr.Entries {
		groups = append(groups, entry.DN)
	}

	return groups, nil
}

// mapGroupsToRoles maps LDAP group DNs to application roles
func (p *LDAPProvider) mapGroupsToRoles(groups []string) []string {
	if len(p.config.RoleMapping) == 0 {
		return groups
	}

	roles := make([]string, 0, len(groups))
	for _, groupDN := range groups {
		if role, ok := p.config.RoleMapping[groupDN]; ok {
			roles = append(roles, role)
		}
	}

	return roles
}

// OpenAPIScheme returns the OpenAPI security scheme
func (p *LDAPProvider) OpenAPIScheme() auth.SecurityScheme {
	return auth.SecurityScheme{
		Type:        string(auth.SecurityTypeHTTP),
		Scheme:      "basic",
		Description: "LDAP/Active Directory authentication via HTTP Basic Auth",
	}
}

// Middleware returns the authentication middleware
func (p *LDAPProvider) Middleware() forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			authCtx, err := p.Authenticate(ctx.Context(), ctx.Request())
			if err != nil {
				p.logger.Debug("ldap middleware authentication failed",
					forge.F("path", ctx.Request().URL.Path),
					forge.F("error", err),
				)
				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			// Store auth context in forge context
			ctx.Set("auth_context", authCtx)
			return next(ctx)
		}
	}
}

// Close closes the LDAP connection pool
func (p *LDAPProvider) Close() error {
	return p.connPool.close()
}

// --- Connection Pool Methods ---

func (pool *ldapConnPool) createConnection() (*ldap.Conn, error) {
	addr := fmt.Sprintf("%s:%d", pool.config.Host, pool.config.Port)

	var conn *ldap.Conn
	var err error

	// Retry with exponential backoff
	for attempt := 0; attempt < pool.config.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(pool.config.RetryDelay * time.Duration(1<<uint(attempt-1)))
		}

		// Dial with timeout using goroutine
		type result struct {
			conn *ldap.Conn
			err  error
		}
		resultCh := make(chan result, 1)
		go func() {
			c, e := ldap.Dial("tcp", addr)
			resultCh <- result{conn: c, err: e}
		}()

		select {
		case res := <-resultCh:
			conn, err = res.conn, res.err
		case <-time.After(pool.config.ConnectionTimeout):
			err = fmt.Errorf("connection timeout after %v", pool.config.ConnectionTimeout)
		}

		if err == nil {
			break
		}

		pool.logger.Warn("ldap connection attempt failed",
			forge.F("attempt", attempt+1),
			forge.F("error", err),
		)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to ldap after %d attempts: %w", pool.config.MaxRetries, err)
	}

	// Start TLS if enabled
	if pool.config.UseTLS {
		tlsConfig := &tls.Config{
			ServerName:         pool.config.Host,
			InsecureSkipVerify: pool.config.InsecureSkipVerify,
		}

		if err := conn.StartTLS(tlsConfig); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to start tls: %w", err)
		}
	}

	// Set timeouts
	conn.SetTimeout(pool.config.RequestTimeout)

	return conn, nil
}

func (pool *ldapConnPool) getConn(ctx context.Context) (*ldap.Conn, error) {
	pool.mu.Lock()
	if pool.closed {
		pool.mu.Unlock()
		return nil, fmt.Errorf("connection pool is closed")
	}
	pool.mu.Unlock()

	select {
	case conn := <-pool.conns:
		// Test connection health
		if err := pool.healthCheck(conn); err != nil {
			conn.Close()
			return pool.createConnection()
		}
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Pool exhausted, create new connection
		return pool.createConnection()
	}
}

func (pool *ldapConnPool) putConn(conn *ldap.Conn) {
	pool.mu.Lock()
	if pool.closed {
		pool.mu.Unlock()
		conn.Close()
		return
	}
	pool.mu.Unlock()

	select {
	case pool.conns <- conn:
		// Connection returned to pool
	default:
		// Pool is full, close connection
		conn.Close()
	}
}

func (pool *ldapConnPool) healthCheck(conn *ldap.Conn) error {
	// Simple health check - try to bind
	return conn.Bind(pool.config.BindDN, pool.config.BindPassword)
}

func (pool *ldapConnPool) close() error {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	if pool.closed {
		return nil
	}

	pool.closed = true
	close(pool.conns)

	// Close all connections in pool
	for conn := range pool.conns {
		conn.Close()
	}

	return nil
}

// --- Cache Methods ---

func (c *ldapCache) get(username, password string) *auth.AuthContext {
	if c == nil {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	key := c.cacheKey(username, password)
	entry, ok := c.entries[key]
	if !ok {
		return nil
	}

	if time.Now().After(entry.expiresAt) {
		return nil
	}

	return entry.authCtx
}

func (c *ldapCache) set(username, password string, authCtx *auth.AuthContext) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.cacheKey(username, password)
	c.entries[key] = &cacheEntry{
		authCtx:   authCtx,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *ldapCache) cacheKey(username, password string) string {
	// Simple hash - in production, use proper hashing
	return fmt.Sprintf("%s:%s", username, password)
}

func (c *ldapCache) cleanup() {
	ticker := time.NewTicker(c.ttl / 2)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, entry := range c.entries {
			if now.After(entry.expiresAt) {
				delete(c.entries, key)
			}
		}
		c.mu.Unlock()
	}
}

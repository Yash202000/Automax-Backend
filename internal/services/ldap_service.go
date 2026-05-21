package services

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/go-ldap/ldap/v3"
)

// LDAPService provides LDAP/Active Directory integration
type LDAPService interface {
	// Authenticate authenticates a user against LDAP/AD
	Authenticate(ctx context.Context, username, password string) (*LDAPUserInfo, error)
	// SearchUser searches for a user in LDAP/AD
	SearchUser(ctx context.Context, username string) (*LDAPUserInfo, error)
	// SearchGroup searches for groups in LDAP/AD
	SearchGroup(ctx context.Context, userDN string) ([]LDAPGroupInfo, error)
	// SyncUser syncs LDAP user data to local database
	SyncUser(ctx context.Context, ldapUser *LDAPUserInfo) (*models.User, error)
	// TestConnection tests the LDAP connection
	TestConnection(ctx context.Context) error
	// FetchUserList retrieves all users from AD with pagination
	FetchUserList(ctx context.Context) ([]LDAPUserListItem, error)
	// Close closes the LDAP connection pool
	Close()
}

// LDAPUserInfo contains user information from LDAP/AD
type LDAPUserInfo struct {
	DN          string
	Username    string
	Email       string
	FirstName   string
	LastName    string
	Phone       string
	Department  string
	Title       string
	Manager     string
	MemberOf    []string
	ObjectGUID  string
	SID         string
	Enabled     bool
	LastLogon   time.Time
	CreatedAt   time.Time
}

// LDAPGroupInfo contains group information from LDAP/AD
type LDAPGroupInfo struct {
	DN          string
	Name        string
	Description string
	Members     []string
}

// LDAPUserListItem contains summary info for listing AD users
type LDAPUserListItem struct {
	DN          string `json:"dn"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	UPN         string `json:"upn"`
	Email       string `json:"email"`
}

// ldapService implements LDAPService
type ldapService struct {
	config *config.LDAPConfig
	pool   *ldapConnectionPool
}

// ldapConnectionPool manages LDAP connections
type ldapConnectionPool struct {
	conn     *ldap.Conn
	mu       chan struct{}
	closed   bool
	config   *config.LDAPConfig
}

const (
	defaultPoolSize = 5
	dialTimeout     = 10 * time.Second
	requestTimeout  = 30 * time.Second
)

// NewLDAPService creates a new LDAP service
func NewLDAPService(cfg *config.Config) (LDAPService, error) {
	if !cfg.LDAP.Enabled {
		return &ldapService{config: &cfg.LDAP, pool: nil}, nil
	}

	pool, err := newLDAPConnectionPool(cfg.LDAP, defaultPoolSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create LDAP connection pool: %w", err)
	}

	return &ldapService{
		config: &cfg.LDAP,
		pool:   pool,
	}, nil
}

// newLDAPConnectionPool creates a pool of LDAP connections
func newLDAPConnectionPool(cfg config.LDAPConfig, size int) (*ldapConnectionPool, error) {
	pool := &ldapConnectionPool{
		mu:     make(chan struct{}, size),
		config: &cfg,
	}

	// Create initial connection
	conn, err := pool.dial()
	if err != nil {
		return nil, err
	}
	pool.conn = conn

	// Fill the pool semaphore
	for i := 0; i < size; i++ {
		pool.mu <- struct{}{}
	}

	return pool, nil
}

// dial creates a new LDAP connection
func (p *ldapConnectionPool) dial() (*ldap.Conn, error) {
	var conn *ldap.Conn
	var err error

	// Build TLS config
	tlsConfig := &tls.Config{
		InsecureSkipVerify: p.config.InsecureSkipVerify,
	}

	// Check if using LDAPS
	if strings.HasPrefix(p.config.URL, "ldaps://") {
		tlsConfig.ServerName = strings.Split(strings.TrimPrefix(p.config.URL, "ldaps://"), ":")[0]
		conn, err = ldap.DialTLS("tcp", strings.TrimPrefix(p.config.URL, "ldaps://"), tlsConfig)
	} else {
		conn, err = ldap.Dial("tcp", strings.TrimPrefix(p.config.URL, "ldap://"))
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to LDAP server: %w", err)
	}

	// Set timeout
	conn.SetTimeout(dialTimeout)

	// If using plain LDAP, upgrade to StartTLS (required by most AD servers)
	if !strings.HasPrefix(p.config.URL, "ldaps://") {
		host := strings.Split(strings.TrimPrefix(p.config.URL, "ldap://"), ":")[0]
		tlsConfig.ServerName = host
		if err := conn.StartTLS(tlsConfig); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to start TLS: %w", err)
		}
	}

	// If bind credentials are provided, perform bind
	if p.config.BindDN != "" && p.config.BindPassword != "" {
		if err := conn.Bind(p.config.BindDN, p.config.BindPassword); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to bind to LDAP: %w", err)
		}
	}

	return conn, nil
}

// getConnection gets a connection from the pool
func (p *ldapConnectionPool) getConnection() (*ldap.Conn, error) {
	if p.closed {
		return nil, errors.New("connection pool is closed")
	}

	// Wait for available slot
	select {
	case <-p.mu:
		// Check if connection is still valid
		if p.conn == nil || p.conn.IsClosing() {
			// Reconnect
			newConn, err := p.dial()
			if err != nil {
				p.mu <- struct{}{} // Return the slot
				return nil, err
			}
			p.conn = newConn
		}
		return p.conn, nil
	case <-time.After(requestTimeout):
		return nil, errors.New("timeout waiting for LDAP connection")
	}
}

// releaseConnection returns a connection to the pool
func (p *ldapConnectionPool) releaseConnection() {
	select {
	case p.mu <- struct{}{}:
	default:
		// Pool is full, discard
	}
}

// Close closes the connection pool
func (p *ldapConnectionPool) Close() {
	p.closed = true
	if p.conn != nil {
		p.conn.Close()
	}
}

// FetchUserList retrieves all users from Active Directory
func (s *ldapService) FetchUserList(ctx context.Context) ([]LDAPUserListItem, error) {
	if s.pool == nil {
		return nil, errors.New("LDAP is not enabled")
	}

	conn, err := s.pool.getConnection()
	if err != nil {
		return nil, err
	}
	defer s.pool.releaseConnection()

	filter := "(&(objectCategory=person)(objectClass=user))"
	attrs := []string{
		"dn",
		"sAMAccountName",
		"displayName",
		"userPrincipalName",
		"mail",
	}

	searchRequest := ldap.NewSearchRequest(
		s.config.UserSearchBase,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		int(requestTimeout.Seconds()),
		false,
		filter,
		attrs,
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to search AD users: %w", err)
	}

	users := make([]LDAPUserListItem, 0, len(result.Entries))
	for _, entry := range result.Entries {
		user := LDAPUserListItem{
			DN:          entry.DN,
			Username:    entry.GetAttributeValue("sAMAccountName"),
			DisplayName: entry.GetAttributeValue("displayName"),
			UPN:         entry.GetAttributeValue("userPrincipalName"),
			Email:       entry.GetAttributeValue("mail"),
		}
		users = append(users, user)
	}

	return users, nil
}

// Authenticate authenticates a user against LDAP/AD
func (s *ldapService) Authenticate(ctx context.Context, username, password string) (*LDAPUserInfo, error) {
	if s.pool == nil {
		return nil, errors.New("LDAP is not enabled")
	}

	// First, search for the user
	userInfo, err := s.SearchUser(ctx, username)
	if err != nil {
		return nil, err
	}

	// Create a new connection for authentication (don't use pooled connection for user bind)
	var authConn *ldap.Conn
	tlsConfig := &tls.Config{
		InsecureSkipVerify: s.config.InsecureSkipVerify,
	}
	if strings.HasPrefix(s.config.URL, "ldaps://") {
		tlsConfig.ServerName = strings.Split(strings.TrimPrefix(s.config.URL, "ldaps://"), ":")[0]
		authConn, err = ldap.DialTLS("tcp", strings.TrimPrefix(s.config.URL, "ldaps://"), tlsConfig)
	} else {
		authConn, err = ldap.Dial("tcp", strings.TrimPrefix(s.config.URL, "ldap://"))
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect for authentication: %w", err)
	}
	defer authConn.Close()

	// Upgrade to StartTLS for plain LDAP connections
	if !strings.HasPrefix(s.config.URL, "ldaps://") {
		host := strings.Split(strings.TrimPrefix(s.config.URL, "ldap://"), ":")[0]
		tlsConfig.ServerName = host
		if err := authConn.StartTLS(tlsConfig); err != nil {
			return nil, fmt.Errorf("failed to start TLS for authentication: %w", err)
		}
	}

	// Attempt to bind with user credentials
	if err := authConn.Bind(userInfo.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, errors.New("invalid credentials")
		}
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Successfully authenticated
	return userInfo, nil
}

// SearchUser searches for a user in LDAP/AD
func (s *ldapService) SearchUser(ctx context.Context, username string) (*LDAPUserInfo, error) {
	if s.pool == nil {
		return nil, errors.New("LDAP is not enabled")
	}

	conn, err := s.pool.getConnection()
	if err != nil {
		return nil, err
	}
	defer s.pool.releaseConnection()

	// Replace {{username}} placeholder in filter
	filter := strings.ReplaceAll(s.config.UserSearchFilter, "{{username}}", ldap.EscapeFilter(username))

	// Create search request
	searchRequest := ldap.NewSearchRequest(
		s.config.UserSearchBase,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,              // No size limit
		int(requestTimeout.Seconds()),
		false,          // Types only false (we want attributes)
		filter,
		s.getUserAttributes(),
		nil,
	)

	// Execute search
	result, err := conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to search user: %w", err)
	}

	if len(result.Entries) == 0 {
		return nil, errors.New("user not found")
	}

	if len(result.Entries) > 1 {
		return nil, errors.New("multiple users found")
	}

	entry := result.Entries[0]
	return s.parseUserEntry(entry), nil
}

// getUserAttributes returns the list of attributes to fetch for users
func (s *ldapService) getUserAttributes() []string {
	return []string{
		"dn",
		"sAMAccountName",
		"userPrincipalName",
		"mail",
		"givenName",
		"sn",
		"displayName",
		"telephoneNumber",
		"mobile",
		"department",
		"title",
		"manager",
		"memberOf",
		"objectGUID",
		"objectSid",
		"userAccountControl",
		"lastLogon",
		"whenCreated",
		"cn",
	}
}

// parseUserEntry parses an LDAP entry into LDAPUserInfo
func (s *ldapService) parseUserEntry(entry *ldap.Entry) *LDAPUserInfo {
	user := &LDAPUserInfo{
		DN:         entry.DN,
		Username:   entry.GetAttributeValue("sAMAccountName"),
		Email:      entry.GetAttributeValue("mail"),
		FirstName:  entry.GetAttributeValue("givenName"),
		LastName:   entry.GetAttributeValue("sn"),
		Phone:      entry.GetAttributeValue("telephoneNumber"),
		Department: entry.GetAttributeValue("department"),
		Title:      entry.GetAttributeValue("title"),
		Manager:    entry.GetAttributeValue("manager"),
		MemberOf:   entry.GetAttributeValues("memberOf"),
		ObjectGUID: formatGUID(entry.GetRawAttributeValues("objectGUID")[0]),
		SID:        formatSID(entry.GetRawAttributeValues("objectSid")[0]),
	}

	// Fallback for username
	if user.Username == "" {
		user.Username = entry.GetAttributeValue("userPrincipalName")
		if user.Username == "" {
			user.Username = entry.GetAttributeValue("cn")
		}
	}

	// Fallback for email (construct from UPN if not available)
	if user.Email == "" {
		upn := entry.GetAttributeValue("userPrincipalName")
		if upn != "" && strings.Contains(upn, "@") {
			user.Email = upn
		}
	}

	// Fallback for name
	if user.FirstName == "" {
		user.FirstName = entry.GetAttributeValue("displayName")
	}

	// Parse userAccountControl to check if enabled
	uac := entry.GetAttributeValue("userAccountControl")
	if uac != "" {
		// ACCOUNTDISABLE = 0x0002
		user.Enabled = (getIntFromLDAP(uac) & 0x0002) == 0
	} else {
		user.Enabled = true // Assume enabled if not specified
	}

	// Parse lastLogon (Windows timestamp)
	lastLogon := entry.GetRawAttributeValues("lastLogon")
	if len(lastLogon) > 0 {
		user.LastLogon = windowsTimeToTime(lastLogon[0])
	}

	// Parse whenCreated
	whenCreated := entry.GetAttributeValue("whenCreated")
	if whenCreated != "" {
		if t, err := time.Parse("20060102150405Z", whenCreated); err == nil {
			user.CreatedAt = t
		}
	}

	return user
}

// SearchGroup searches for groups in LDAP/AD
func (s *ldapService) SearchGroup(ctx context.Context, userDN string) ([]LDAPGroupInfo, error) {
	if s.pool == nil {
		return nil, errors.New("LDAP is not enabled")
	}

	conn, err := s.pool.getConnection()
	if err != nil {
		return nil, err
	}
	defer s.pool.releaseConnection()

	// Replace {{userDN}} placeholder in filter
	filter := strings.ReplaceAll(s.config.GroupSearchFilter, "{{userDN}}", userDN)

	// Create search request
	searchRequest := ldap.NewSearchRequest(
		s.config.GroupSearchBase,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,              // No size limit
		int(requestTimeout.Seconds()),
		false,          // Types only false
		filter,
		s.getGroupAttributes(),
		nil,
	)

	// Execute search
	result, err := conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to search groups: %w", err)
	}

	groups := make([]LDAPGroupInfo, 0, len(result.Entries))
	for _, entry := range result.Entries {
		groups = append(groups, s.parseGroupEntry(entry))
	}

	return groups, nil
}

// getGroupAttributes returns the list of attributes to fetch for groups
func (s *ldapService) getGroupAttributes() []string {
	return []string{
		"dn",
		"cn",
		"name",
		"description",
		"member",
	}
}

// parseGroupEntry parses an LDAP entry into LDAPGroupInfo
func (s *ldapService) parseGroupEntry(entry *ldap.Entry) LDAPGroupInfo {
	return LDAPGroupInfo{
		DN:          entry.DN,
		Name:        entry.GetAttributeValue("cn"),
		Description: entry.GetAttributeValue("description"),
		Members:     entry.GetAttributeValues("member"),
	}
}

// SyncUser syncs LDAP user data to local database
func (s *ldapService) SyncUser(ctx context.Context, ldapUser *LDAPUserInfo) (*models.User, error) {
	// This would typically be implemented with a user repository
	// For now, return the basic structure - actual implementation
	// would depend on your repository pattern
	return nil, errors.New("SyncUser requires repository integration")
}

// TestConnection tests the LDAP connection
func (s *ldapService) TestConnection(ctx context.Context) error {
	if s.pool == nil {
		return errors.New("LDAP is not enabled")
	}

	conn, err := s.pool.getConnection()
	if err != nil {
		return err
	}
	defer s.pool.releaseConnection()

	// Perform a simple search to verify connection
	searchRequest := ldap.NewSearchRequest(
		s.config.BaseDN,
		ldap.ScopeBaseObject,
		ldap.NeverDerefAliases,
		0,
		5, // 5 seconds
		false,
		"(objectClass=*)",
		[]string{"objectClass"},
		nil,
	)

	_, err = conn.Search(searchRequest)
	if err != nil {
		return fmt.Errorf("LDAP connection test failed: %w", err)
	}

	return nil
}

// Close closes the LDAP connection pool
func (s *ldapService) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Helper functions

func getIntFromLDAP(s string) int {
	var result int
	fmt.Sscanf(s, "%d", &result)
	return result
}

// formatGUID formats the binary GUID to a standard string representation
func formatGUID(guidBytes []byte) string {
	if len(guidBytes) == 0 {
		return ""
	}
	// LDAP returns GUID in little-endian format, convert to standard format
	// Standard: 8-4-4-4-12 hex digits
	// LDAP: first 4 bytes are little-endian, next 2 are little-endian, etc.
	if len(guidBytes) >= 16 {
		return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
			uint32(guidBytes[3])<<24|uint32(guidBytes[2])<<16|uint32(guidBytes[1])<<8|uint32(guidBytes[0]),
			uint16(guidBytes[5])<<8|uint16(guidBytes[4]),
			uint16(guidBytes[7])<<8|uint16(guidBytes[6]),
			uint16(guidBytes[8])<<8|uint16(guidBytes[9]),
			uint64(guidBytes[10])<<40|uint64(guidBytes[11])<<32|uint64(guidBytes[12])<<24|uint64(guidBytes[13])<<16|uint64(guidBytes[14])<<8|uint64(guidBytes[15]))
	}
	return fmt.Sprintf("%x", guidBytes)
}

// formatSID formats the binary SID to a standard string representation
func formatSID(sidBytes []byte) string {
	if len(sidBytes) == 0 {
		return ""
	}
	// SID format: S-1-5-21-...
	// First byte: revision
	// Second byte: sub-authority count
	// Next 6 bytes: identifier authority
	// Remaining bytes: sub-authorities (4 bytes each, little-endian)
	if len(sidBytes) < 8 {
		return fmt.Sprintf("%x", sidBytes)
	}

	revision := sidBytes[0]
	subAuthCount := sidBytes[1]
	
	// Identifier authority (big-endian)
	var idAuth uint64
	for i := 0; i < 6; i++ {
		idAuth = (idAuth << 8) | uint64(sidBytes[2+i])
	}

	sid := fmt.Sprintf("S-%d-%d", revision, idAuth)

	// Sub-authorities (little-endian)
	offset := 8
	for i := 0; i < int(subAuthCount) && offset+4 <= len(sidBytes); i++ {
		subAuth := uint32(sidBytes[offset]) | uint32(sidBytes[offset+1])<<8 | uint32(sidBytes[offset+2])<<16 | uint32(sidBytes[offset+3])<<24
		sid += fmt.Sprintf("-%d", subAuth)
		offset += 4
	}

	return sid
}

// windowsTimeToTime converts Windows FILETIME to Go time.Time
func windowsTimeToTime(windowsTime []byte) time.Time {
	if len(windowsTime) != 8 {
		return time.Time{}
	}

	// Convert to int64
	var fileTime int64
	for i := 0; i < 8; i++ {
		fileTime |= int64(windowsTime[i]) << (uint(i) * 8)
	}

	// Windows FILETIME is 100-nanosecond intervals since January 1, 1601 UTC
	// Convert to Unix time (seconds since January 1, 1970 UTC)
	if fileTime > 0 {
		unixTime := fileTime/10000000 - 11644473600
		return time.Unix(unixTime, 0)
	}

	return time.Time{}
}

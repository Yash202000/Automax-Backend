package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/redis/go-redis/v9"
)

const (
	redisTokenKey        = "documenta:oauth_token"
	redisFolderKeyPrefix = "documenta:folder:" // e.g., documenta:folder:automax::Goal Management
	tokenSafetyMargin    = 60                  // seconds before expiry to refresh
	defaultOnBehalf      = "system@automax.local"
)

// ════════════════════════════════════════════════════
// HTTP Client Implementation
// ════════════════════════════════════════════════════

type httpDocumentaClient struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client
	redisClient  *redis.Client

	// In-memory token cache
	mu            sync.RWMutex
	cachedToken   string
	tokenExpiry   time.Time
	workspaceUUID string // extracted from OAuth JWT on first token fetch
}

// NewHTTPDocumentaClient creates a real Documenta/MyDocs HTTP client.
func NewHTTPDocumentaClient(cfg config.DocumentaConfig, redisClient *redis.Client) DocumentaClient {
	return &httpDocumentaClient{
		baseURL:      cfg.BaseURL + "/api/v1",
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		redisClient:  redisClient,
	}
}

// ──────────────────────────────────────────────────
// OAuth Token Management (3-tier cache)
// ──────────────────────────────────────────────────

func (c *httpDocumentaClient) getToken(ctx context.Context) (string, error) {
	// Tier 1: In-memory
	c.mu.RLock()
	if c.cachedToken != "" && time.Now().Before(c.tokenExpiry) {
		token := c.cachedToken
		c.mu.RUnlock()
		c.extractWorkspaceUUID(token)
		return token, nil
	}
	c.mu.RUnlock()

	// Tier 2: Redis
	if c.redisClient != nil {
		val, err := c.redisClient.Get(ctx, redisTokenKey).Result()
		if err == nil && val != "" {
			ttl, _ := c.redisClient.TTL(ctx, redisTokenKey).Result()
			c.mu.Lock()
			c.cachedToken = val
			c.tokenExpiry = time.Now().Add(ttl)
			c.mu.Unlock()
			c.extractWorkspaceUUID(val)
			return val, nil
		}
	}

	// Tier 3: Fresh token
	return c.fetchNewToken(ctx)
}

// extractWorkspaceUUID decodes the JWT payload to get workspace_uuid.
// Called on every token fetch so workspace changes in MyDocs are picked up dynamically.
func (c *httpDocumentaClient) extractWorkspaceUUID(token string) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return
	}
	payload := parts[1]
	if m := len(payload) % 4; m != 0 {
		payload += strings.Repeat("=", 4-m)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return
	}
	var claims struct {
		WorkspaceUUID string `json:"workspace_uuid"`
	}
	if json.Unmarshal(decoded, &claims) == nil && claims.WorkspaceUUID != "" {
		if c.workspaceUUID != claims.WorkspaceUUID {
			if c.workspaceUUID != "" {
				log.Printf("[DOCUMENTA] Workspace UUID changed: %s → %s", c.workspaceUUID, claims.WorkspaceUUID)
			} else {
				log.Printf("[DOCUMENTA] Resolved workspace UUID: %s", claims.WorkspaceUUID)
			}
			c.workspaceUUID = claims.WorkspaceUUID
		}
	}
}

func (c *httpDocumentaClient) fetchNewToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.cachedToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.cachedToken, nil
	}

	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/oauth/token", bytes.NewBufferString(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("documenta: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("documenta: token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("documenta: token request failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("documenta: decode token response: %w", err)
	}

	if result.AccessToken == "" {
		return "", fmt.Errorf("documenta: empty access token in response")
	}

	token := result.AccessToken
	expiresIn := result.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}

	ttl := time.Duration(expiresIn-tokenSafetyMargin) * time.Second
	if ttl < 30*time.Second {
		ttl = 30 * time.Second
	}

	c.cachedToken = token
	c.tokenExpiry = time.Now().Add(ttl)
	c.extractWorkspaceUUID(token)

	// Store in Redis
	if c.redisClient != nil {
		c.redisClient.Set(ctx, redisTokenKey, token, ttl)
	}

	log.Printf("[DOCUMENTA] OAuth token acquired, expires in %ds", expiresIn)
	return token, nil
}

// resolveWorkspace returns the workspace UUID if resolved, otherwise falls back to the slug.
func (c *httpDocumentaClient) resolveWorkspace(slug string) string {
	if c.workspaceUUID != "" {
		return c.workspaceUUID
	}
	return slug
}

// ──────────────────────────────────────────────────
// HTTP Helpers
// ──────────────────────────────────────────────────

type onBehalfKey struct{}

// ContextWithOnBehalf stores the user email in context for X-On-Behalf-Of.
func ContextWithOnBehalf(ctx context.Context, email string) context.Context {
	return context.WithValue(ctx, onBehalfKey{}, email)
}

func getOnBehalf(ctx context.Context) string {
	if v, ok := ctx.Value(onBehalfKey{}).(string); ok && v != "" {
		return v
	}
	return defaultOnBehalf
}

func (c *httpDocumentaClient) doRequest(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("documenta: create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-On-Behalf-Of", getOnBehalf(ctx))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return c.httpClient.Do(req)
}

func (c *httpDocumentaClient) doJSON(ctx context.Context, method, path string, payload interface{}) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("documenta: marshal payload: %w", err)
		}
		body = bytes.NewReader(data)
	}
	return c.doRequest(ctx, method, path, body, "application/json")
}

// parseResponse decodes the standard MyDocs envelope { success, data }.
func parseResponse[T any](resp *http.Response) (*T, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("documenta: request failed (%d): %s", resp.StatusCode, string(body))
	}

	var envelope struct {
		Success bool `json:"success"`
		Data    T    `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("documenta: decode response: %w", err)
	}
	return &envelope.Data, nil
}

// ──────────────────────────────────────────────────
// Folder & File Operations
// ──────────────────────────────────────────────────

func (c *httpDocumentaClient) CreateFolder(ctx context.Context, workspaceName string, parentID string, name string) (string, error) {
	if _, err := c.getToken(ctx); err != nil {
		return "", err
	}
	payload := map[string]string{
		"workspaceUuid": c.resolveWorkspace(workspaceName),
		"name":          name,
	}
	if parentID != "" {
		payload["parentUuid"] = parentID
	}

	log.Printf("[DOCUMENTA] CreateFolder: workspace=%s parent=%s name=%q", c.resolveWorkspace(workspaceName), parentID, name)
	resp, err := c.doJSON(ctx, http.MethodPost, "/files/folder", payload)
	if err != nil {
		log.Printf("[DOCUMENTA] CreateFolder doJSON error: %v", err)
		return "", err
	}

	type folderResp struct {
		UUID string `json:"uuid"`
	}
	result, err := parseResponse[folderResp](resp)
	if err != nil {
		log.Printf("[DOCUMENTA] CreateFolder parseResponse error: %v", err)
		return "", err
	}
	return result.UUID, nil
}

func (c *httpDocumentaClient) EnsureFolder(ctx context.Context, workspaceName string, parentID string, name string) (string, error) {
	cacheKey := redisFolderKeyPrefix + workspaceName + ":" + parentID + ":" + name

	// Tier 1: Check Redis cache
	if c.redisClient != nil {
		val, err := c.redisClient.Get(ctx, cacheKey).Result()
		if err == nil && val != "" {
			return val, nil
		}
	}

	// Tier 2: List parent folder and look for existing folder by name
	result, err := c.ListFiles(ctx, workspaceName, parentID)
	if err == nil {
		for _, f := range result.Files {
			if f.Type == "folder" && f.Name == name {
				if c.redisClient != nil {
					c.redisClient.Set(ctx, cacheKey, f.UUID, 0) // permanent cache
				}
				return f.UUID, nil
			}
		}
	}

	// Tier 3: Create folder
	folderID, createErr := c.CreateFolder(ctx, workspaceName, parentID, name)
	if createErr != nil {
		return "", fmt.Errorf("ensure folder %q: %w", name, createErr)
	}

	if c.redisClient != nil {
		c.redisClient.Set(ctx, cacheKey, folderID, 0) // permanent cache
	}
	log.Printf("[DOCUMENTA] EnsureFolder: created %q under %q → %s", name, parentID, folderID)
	return folderID, nil
}

func (c *httpDocumentaClient) UploadFile(ctx context.Context, folderID, fileName string, fileData io.Reader, fileSize int64, metadata map[string]string) (string, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// MyDocs multipart upload uses "workspace" (not "workspaceUuid")
	wsUUID := c.workspaceUUID
	if wsUUID != "" {
		if err := writer.WriteField("workspace", wsUUID); err != nil {
			return "", fmt.Errorf("documenta: write workspace field: %w", err)
		}
	}

	// MyDocs file-upload handler reads "parent" (not "parentUuid") from the
	// multipart form. Folder creation uses "parentUuid" in its JSON body — the
	// two endpoints disagree on field naming, don't unify.
	if err := writer.WriteField("parent", folderID); err != nil {
		return "", fmt.Errorf("documenta: write parent field: %w", err)
	}

	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return "", fmt.Errorf("documenta: create form file: %w", err)
	}
	if _, err := io.Copy(part, fileData); err != nil {
		return "", fmt.Errorf("documenta: copy file data: %w", err)
	}

	// Write metadata as JSON field
	if len(metadata) > 0 {
		metaJSON, _ := json.Marshal(metadata)
		if err := writer.WriteField("metadata", string(metaJSON)); err != nil {
			return "", fmt.Errorf("documenta: write metadata field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("documenta: close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/files/upload", &buf)
	if err != nil {
		return "", fmt.Errorf("documenta: create upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-On-Behalf-Of", getOnBehalf(ctx))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("documenta: upload request: %w", err)
	}

	type uploadResp struct {
		UUID string `json:"uuid"`
	}
	result, parseErr := parseResponse[uploadResp](resp)
	if parseErr != nil {
		return "", parseErr
	}
	return result.UUID, nil
}

func (c *httpDocumentaClient) GetPreviewURL(ctx context.Context, fileID string) (string, error) {
	// Return the backend proxy URL — frontend calls our backend, which streams from Documenta
	return fmt.Sprintf("/api/v1/documents/files/%s/preview", fileID), nil
}

func (c *httpDocumentaClient) GetDownloadURL(ctx context.Context, fileID string) (string, error) {
	return fmt.Sprintf("/api/v1/documents/files/%s/download", fileID), nil
}

// DownloadFile streams the raw bytes of a file from MyDocs GET /api/v1/files/{uuid}.
// That endpoint returns the file content (not JSON) with Content-Type,
// Content-Disposition and Content-Length headers set by MyDocs. The caller owns
// the returned reader and must Close it.
func (c *httpDocumentaClient) DownloadFile(ctx context.Context, fileID string) (io.ReadCloser, *DmsFile, error) {
	if fileID == "" {
		return nil, nil, fmt.Errorf("documenta: download file: empty fileID")
	}
	resp, err := c.doRequest(ctx, http.MethodGet, "/files/"+fileID, nil, "")
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, nil, fmt.Errorf("documenta: download file failed (%d): %s", resp.StatusCode, string(body))
	}

	// Pull metadata from the response headers. MyDocs sets Content-Type,
	// Content-Length and Content-Disposition: attachment; filename="..." on this
	// endpoint — see mydocs/gateway/rest/server.go handleGetFile.
	info := &DmsFile{
		UUID:     fileID,
		Type:     "file",
		MimeType: resp.Header.Get("Content-Type"),
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if n, perr := strconv.ParseInt(cl, 10, 64); perr == nil {
			info.Size = n
		}
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, perr := mime.ParseMediaType(cd); perr == nil {
			if fn := params["filename"]; fn != "" {
				info.Name = fn
			}
		}
	}
	return resp.Body, info, nil
}

func (c *httpDocumentaClient) UpdateMetadata(ctx context.Context, fileID string, metadata map[string]string) error {
	return c.SetTags(ctx, fileID, metadata)
}

func (c *httpDocumentaClient) DeleteFile(ctx context.Context, fileID string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, "/files/"+fileID, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("documenta: delete failed (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// ──────────────────────────────────────────────────
// Browsing & Search
// ──────────────────────────────────────────────────

func (c *httpDocumentaClient) ListFiles(ctx context.Context, workspaceSlug string, parentID string) (*DmsListResult, error) {
	// Ensure token + workspace UUID are resolved before building params
	if _, err := c.getToken(ctx); err != nil {
		return nil, err
	}
	params := url.Values{"workspace": {c.resolveWorkspace(workspaceSlug)}}
	if parentID != "" {
		params.Set("parent", parentID)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/files?"+params.Encode(), nil, "")
	if err != nil {
		return nil, err
	}

	// MyDocs returns { success, data: { files: [...], settings: {...}, workspaceRole: "..." } }
	// Its file objects use camelCase (parentUuid, sizeBytes, mimeType, createdAt).
	type mdFile struct {
		UUID       string `json:"uuid"`
		Name       string `json:"name"`
		Type       string `json:"type"`
		SizeBytes  int64  `json:"sizeBytes"`
		MimeType   string `json:"mimeType"`
		ParentUUID string `json:"parentUuid"`
		Path       string `json:"path"`
		CreatedAt  string `json:"createdAt"`
		UpdatedAt  string `json:"updatedAt"`
	}
	type listData struct {
		Files []mdFile `json:"files"`
	}
	result, parseErr := parseResponse[listData](resp)
	if parseErr != nil {
		return nil, parseErr
	}
	files := make([]DmsFile, len(result.Files))
	for i, f := range result.Files {
		files[i] = DmsFile{
			UUID: f.UUID, Name: f.Name, Type: f.Type, Size: f.SizeBytes, MimeType: f.MimeType,
			Parent: f.ParentUUID, ParentUUID: f.ParentUUID, Path: f.Path,
			CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
		}
	}
	return &DmsListResult{Files: files}, nil
}

func (c *httpDocumentaClient) SearchFiles(ctx context.Context, workspaceSlug string, query string) (*DmsSearchResult, error) {
	if _, err := c.getToken(ctx); err != nil {
		return nil, err
	}
	payload := map[string]string{
		"workspace": c.resolveWorkspace(workspaceSlug),
		"query":     query,
	}

	resp, err := c.doJSON(ctx, http.MethodPost, "/search", payload)
	if err != nil {
		return nil, err
	}
	return parseSearchResponse(resp)
}

// parseSearchResponse converts a MyDocs /search response (camelCase file objects)
// into the snake_case DmsSearchResult the frontend expects.
func parseSearchResponse(resp *http.Response) (*DmsSearchResult, error) {
	type mdFile struct {
		UUID       string `json:"uuid"`
		Name       string `json:"name"`
		Type       string `json:"type"`
		SizeBytes  int64  `json:"sizeBytes"`
		MimeType   string `json:"mimeType"`
		ParentUUID string `json:"parentUuid"`
		Path       string `json:"path"`
		CreatedAt  string `json:"createdAt"`
		UpdatedAt  string `json:"updatedAt"`
	}
	type searchData struct {
		Files []mdFile `json:"files"`
		Total int      `json:"total"`
	}
	result, err := parseResponse[searchData](resp)
	if err != nil {
		return nil, err
	}
	files := make([]DmsFile, len(result.Files))
	for i, f := range result.Files {
		files[i] = DmsFile{
			UUID: f.UUID, Name: f.Name, Type: f.Type, Size: f.SizeBytes, MimeType: f.MimeType,
			Parent: f.ParentUUID, ParentUUID: f.ParentUUID, Path: f.Path,
			CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
		}
	}
	return &DmsSearchResult{Files: files, Total: result.Total}, nil
}

func (c *httpDocumentaClient) SearchFilesWithTags(ctx context.Context, workspaceSlug string, query string, tags map[string]string) (*DmsSearchResult, error) {
	if _, err := c.getToken(ctx); err != nil {
		return nil, err
	}
	payload := map[string]interface{}{
		"workspace": c.resolveWorkspace(workspaceSlug),
		"query":     query,
	}
	if len(tags) > 0 {
		payload["tags"] = tags
	}

	resp, err := c.doJSON(ctx, http.MethodPost, "/search", payload)
	if err != nil {
		return nil, err
	}
	return parseSearchResponse(resp)
}

func (c *httpDocumentaClient) GetFileInfo(ctx context.Context, fileID string) (*DmsFile, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/files/info/"+fileID, nil, "")
	if err != nil {
		return nil, err
	}

	// MyDocs uses camelCase field names; intermediate struct bridges the
	// upstream payload to our snake_case DmsFile contract.
	type mdFileInfo struct {
		UUID       string `json:"uuid"`
		Name       string `json:"name"`
		Type       string `json:"type"`
		SizeBytes  int64  `json:"sizeBytes"`
		MimeType   string `json:"mimeType"`
		ParentUUID string `json:"parentUuid"`
		Path       string `json:"path"`
		CreatedAt  string `json:"createdAt"`
		UpdatedAt  string `json:"updatedAt"`
	}
	info, parseErr := parseResponse[mdFileInfo](resp)
	if parseErr != nil {
		return nil, parseErr
	}
	return &DmsFile{
		UUID:       info.UUID,
		Name:       info.Name,
		Type:       info.Type,
		Size:       info.SizeBytes,
		MimeType:   info.MimeType,
		Parent:     info.ParentUUID, // legacy alias
		ParentUUID: info.ParentUUID,
		Path:       info.Path,
		CreatedAt:  info.CreatedAt,
		UpdatedAt:  info.UpdatedAt,
	}, nil
}

// GetFileBreadcrumb returns the folder chain from workspace root to the file's
// parent folder (the file itself is NOT included). Walks up via GetFileInfo.
// Returns an empty slice if the file is at workspace root.
func (c *httpDocumentaClient) GetFileBreadcrumb(ctx context.Context, fileID string) ([]DmsBreadcrumbEntry, error) {
	start, err := c.GetFileInfo(ctx, fileID)
	if err != nil {
		return nil, err
	}
	// Walk up: collect ancestors (not the file itself).
	var chain []DmsBreadcrumbEntry
	parentID := start.ParentUUID
	const maxDepth = 32 // defensive limit to avoid runaway walks
	for i := 0; parentID != "" && i < maxDepth; i++ {
		parent, err := c.GetFileInfo(ctx, parentID)
		if err != nil {
			return nil, fmt.Errorf("documenta: resolve breadcrumb at %s: %w", parentID, err)
		}
		// Prepend so result is ordered root → parent.
		chain = append([]DmsBreadcrumbEntry{{UUID: parent.UUID, Name: parent.Name}}, chain...)
		parentID = parent.ParentUUID
	}
	return chain, nil
}

// ──────────────────────────────────────────────────
// Comments
// ──────────────────────────────────────────────────

func (c *httpDocumentaClient) GetComments(ctx context.Context, fileID string) ([]DmsComment, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/comments?node="+fileID, nil, "")
	if err != nil {
		return nil, err
	}

	// MyDocs returns { success, data: [ { uuid, content, ... } ] }
	type mdComment struct {
		UUID       string `json:"uuid"`
		Content    string `json:"content"`
		CreatedAt  string `json:"createdAt"`
		AuthorUUID string `json:"authorUuid"`
		AuthorName string `json:"authorName"`
	}
	result, parseErr := parseResponse[[]mdComment](resp)
	if parseErr != nil {
		return nil, parseErr
	}
	if result == nil {
		return []DmsComment{}, nil
	}
	comments := make([]DmsComment, len(*result))
	for i, mc := range *result {
		comments[i] = DmsComment{
			ID:        mc.UUID,
			FileID:    fileID,
			Author:    mc.AuthorName,
			Content:   mc.Content,
			CreatedAt: mc.CreatedAt,
		}
	}
	return comments, nil
}

func (c *httpDocumentaClient) AddComment(ctx context.Context, fileID string, content string) error {
	payload := map[string]string{
		"content": content,
	}

	resp, err := c.doJSON(ctx, http.MethodPost, "/comments?node="+fileID, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("documenta: add comment failed (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// ──────────────────────────────────────────────────
// Tags
// ──────────────────────────────────────────────────

func (c *httpDocumentaClient) GetTags(ctx context.Context, fileID string) (map[string]string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/metadata?node="+fileID, nil, "")
	if err != nil {
		return nil, err
	}

	// MyDocs returns { success, data: [ { key, value, namespace, ... } ] }
	type mdEntry struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	result, parseErr := parseResponse[[]mdEntry](resp)
	if parseErr != nil {
		return nil, parseErr
	}
	tags := make(map[string]string)
	if result != nil {
		for _, entry := range *result {
			tags[entry.Key] = entry.Value
		}
	}
	return tags, nil
}

func (c *httpDocumentaClient) SetTags(ctx context.Context, fileID string, tags map[string]string) error {
	// MyDocs POST /metadata/bulk expects { nodeUuid, entries: [ { namespace, key, value } ] }
	type metadataEntry struct {
		Namespace string `json:"namespace"`
		Key       string `json:"key"`
		Value     string `json:"value"`
		ValueType string `json:"valueType"`
	}
	entries := make([]metadataEntry, 0, len(tags))
	for k, v := range tags {
		entries = append(entries, metadataEntry{
			Namespace: "user",
			Key:       k,
			Value:     v,
			ValueType: "string",
		})
	}

	payload := map[string]interface{}{
		"nodeUuid": fileID,
		"entries":  entries,
	}

	resp, err := c.doJSON(ctx, http.MethodPost, "/metadata/bulk", payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("documenta: set tags failed (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// ──────────────────────────────────────────────────
// Versions
// ──────────────────────────────────────────────────

func (c *httpDocumentaClient) ListVersions(ctx context.Context, fileID string) ([]DmsVersion, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/versions?node="+fileID, nil, "")
	if err != nil {
		return nil, err
	}

	// MyDocs returns { success, data: [ { uuid, nodeUuid, versionNumber, ... } ] }
	type mdVersion struct {
		UUID          string `json:"uuid"`
		NodeUUID      string `json:"nodeUuid"`
		VersionNumber int    `json:"versionNumber"`
		Size          int64  `json:"size"`
		Description   string `json:"description"`
		Source        string `json:"source"`
		CreatedBy     string `json:"createdBy"`
		CreatedByName string `json:"createdByName"`
		CreatedAt     string `json:"createdAt"`
		IsCurrent     bool   `json:"isCurrent"`
		StorageKey    string `json:"storageKey"`
	}
	result, parseErr := parseResponse[[]mdVersion](resp)
	if parseErr != nil {
		return nil, parseErr
	}
	if result == nil {
		return []DmsVersion{}, nil
	}
	versions := make([]DmsVersion, len(*result))
	for i, mv := range *result {
		versions[i] = DmsVersion{
			UUID:          mv.UUID,
			NodeUUID:      mv.NodeUUID,
			VersionNumber: mv.VersionNumber,
			Size:          mv.Size,
			Description:   mv.Description,
			Source:        mv.Source,
			CreatedBy:     mv.CreatedBy,
			CreatedByName: mv.CreatedByName,
			CreatedAt:     mv.CreatedAt,
			IsCurrent:     mv.IsCurrent,
		}
	}
	return versions, nil
}

func (c *httpDocumentaClient) UploadVersion(ctx context.Context, fileID string, fileName string, fileData io.Reader, fileSize int64, description string) (*DmsVersion, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("node", fileID); err != nil {
		return nil, fmt.Errorf("documenta: write node field: %w", err)
	}
	if description != "" {
		if err := writer.WriteField("description", description); err != nil {
			return nil, fmt.Errorf("documenta: write description field: %w", err)
		}
	}

	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return nil, fmt.Errorf("documenta: create form file: %w", err)
	}
	if _, err := io.Copy(part, fileData); err != nil {
		return nil, fmt.Errorf("documenta: copy file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("documenta: close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/versions/upload", &buf)
	if err != nil {
		return nil, fmt.Errorf("documenta: create version upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-On-Behalf-Of", getOnBehalf(ctx))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("documenta: version upload request: %w", err)
	}

	type mdVersion struct {
		UUID          string `json:"uuid"`
		NodeUUID      string `json:"nodeUuid"`
		VersionNumber int    `json:"versionNumber"`
		Size          int64  `json:"size"`
		Description   string `json:"description"`
		Source        string `json:"source"`
		CreatedBy     string `json:"createdBy"`
		CreatedByName string `json:"createdByName"`
		CreatedAt     string `json:"createdAt"`
		IsCurrent     bool   `json:"isCurrent"`
	}
	result, parseErr := parseResponse[mdVersion](resp)
	if parseErr != nil {
		return nil, parseErr
	}
	return &DmsVersion{
		UUID:          result.UUID,
		NodeUUID:      result.NodeUUID,
		VersionNumber: result.VersionNumber,
		Size:          result.Size,
		Description:   result.Description,
		Source:        result.Source,
		CreatedBy:     result.CreatedBy,
		CreatedByName: result.CreatedByName,
		CreatedAt:     result.CreatedAt,
		IsCurrent:     result.IsCurrent,
	}, nil
}

func (c *httpDocumentaClient) DownloadVersion(ctx context.Context, versionUUID string) (io.ReadCloser, string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/versions/"+versionUUID+"/download", nil, "")
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, "", fmt.Errorf("documenta: download version failed (%d): %s", resp.StatusCode, string(body))
	}
	contentType := resp.Header.Get("Content-Type")
	return resp.Body, contentType, nil
}

func (c *httpDocumentaClient) RollbackVersion(ctx context.Context, fileID string, versionUUID string) (*DmsVersion, error) {
	resp, err := c.doJSON(ctx, http.MethodPost, "/versions/rollback", map[string]string{
		"nodeUuid":    fileID,
		"versionUuid": versionUUID,
	})
	if err != nil {
		return nil, err
	}

	type mdVersion struct {
		UUID          string `json:"uuid"`
		NodeUUID      string `json:"nodeUuid"`
		VersionNumber int    `json:"versionNumber"`
		Size          int64  `json:"size"`
		Description   string `json:"description"`
		Source        string `json:"source"`
		CreatedBy     string `json:"createdBy"`
		CreatedByName string `json:"createdByName"`
		CreatedAt     string `json:"createdAt"`
		IsCurrent     bool   `json:"isCurrent"`
	}
	result, parseErr := parseResponse[mdVersion](resp)
	if parseErr != nil {
		return nil, parseErr
	}
	return &DmsVersion{
		UUID:          result.UUID,
		NodeUUID:      result.NodeUUID,
		VersionNumber: result.VersionNumber,
		Size:          result.Size,
		Description:   result.Description,
		Source:        result.Source,
		CreatedBy:     result.CreatedBy,
		CreatedByName: result.CreatedByName,
		CreatedAt:     result.CreatedAt,
		IsCurrent:     result.IsCurrent,
	}, nil
}

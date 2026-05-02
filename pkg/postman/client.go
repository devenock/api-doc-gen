// Package postman provides a minimal HTTP client for the Postman public API
// and helpers for persisting credentials and per-collection cache.
//
// API reference: https://learning.postman.com/docs/developer/postman-api/intro-api/
package postman

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// APIBaseURL is the Postman public API root.
	APIBaseURL = "https://api.getpostman.com"
	// WebBaseURL is used to build a clickable URL to view a collection.
	WebBaseURL = "https://go.postman.co"
)

// Client wraps the Postman REST API. It authenticates via the X-Api-Key header.
type Client struct {
	APIKey     string
	HTTPClient *http.Client
	BaseURL    string
}

// NewClient returns a Client with a 30s timeout and the default base URL.
func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		BaseURL:    APIBaseURL,
	}
}

// MeResponse is the subset of GET /me that we care about (used to validate the key).
type MeResponse struct {
	User struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
		Email    string `json:"email"`
	} `json:"user"`
}

// Me calls GET /me. It is used to validate that an API key is well-formed and authorized.
func (c *Client) Me() (*MeResponse, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/me", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("postman /me request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("postman rejected the API key (HTTP %d). Generate a new key at https://postman.co/settings/me/api-keys", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("postman /me returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var me MeResponse
	if err := json.Unmarshal(body, &me); err != nil {
		return nil, fmt.Errorf("decode /me response: %w", err)
	}
	return &me, nil
}

// CollectionResponse is the relevant subset of POST/PUT /collections responses.
type CollectionResponse struct {
	Collection struct {
		ID   string `json:"id"`
		UID  string `json:"uid"`
		Name string `json:"name"`
	} `json:"collection"`
}

// CreateCollection POSTs the given Postman v2.1 collection JSON.
// collectionJSON must be the body of the collection (with info, item, etc.); the
// envelope {"collection": ...} is added automatically. workspaceUID is optional —
// when empty, Postman uses the user's default workspace.
func (c *Client) CreateCollection(collectionJSON []byte, workspaceUID string) (*CollectionResponse, error) {
	body, err := wrapCollection(collectionJSON)
	if err != nil {
		return nil, err
	}
	url := c.BaseURL + "/collections"
	if workspaceUID != "" {
		url += "?workspace=" + workspaceUID
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	return c.doCollectionRequest(req)
}

// UpdateCollection PUTs to /collections/{uid}. The collection's info._postman_id
// is set/overwritten with uid before sending so Postman doesn't reject it.
func (c *Client) UpdateCollection(uid string, collectionJSON []byte) (*CollectionResponse, error) {
	patched, err := injectPostmanID(collectionJSON, uid)
	if err != nil {
		return nil, err
	}
	body, err := wrapCollection(patched)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPut, c.BaseURL+"/collections/"+uid, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	return c.doCollectionRequest(req)
}

// WebURL returns the user-facing URL for a collection UID.
func WebURL(uid string) string { return WebBaseURL + "/collection/" + uid }

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("X-Api-Key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "apidoc-gen")
}

func (c *Client) doCollectionRequest(req *http.Request) (*CollectionResponse, error) {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("postman API request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("postman rejected the API key (HTTP %d). Run again to re-enter the key, or set APIDOC_POSTMAN_API_KEY", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("postman API returned 404 (collection may have been deleted upstream; remove the cached UID and re-run)")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("postman API returned %d: %s", resp.StatusCode, truncate(string(body), 400))
	}
	var cr CollectionResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("decode collection response: %w", err)
	}
	return &cr, nil
}

func wrapCollection(collectionJSON []byte) ([]byte, error) {
	var inner interface{}
	if err := json.Unmarshal(collectionJSON, &inner); err != nil {
		return nil, fmt.Errorf("invalid collection JSON: %w", err)
	}
	return json.Marshal(map[string]interface{}{"collection": inner})
}

// injectPostmanID sets info._postman_id = uid on the given collection JSON so
// the Postman API does not complain about a missing id on PUT.
func injectPostmanID(collectionJSON []byte, uid string) ([]byte, error) {
	var doc map[string]interface{}
	if err := json.Unmarshal(collectionJSON, &doc); err != nil {
		return nil, fmt.Errorf("invalid collection JSON: %w", err)
	}
	info, ok := doc["info"].(map[string]interface{})
	if !ok {
		info = map[string]interface{}{}
	}
	info["_postman_id"] = uid
	doc["info"] = info
	return json.Marshal(doc)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

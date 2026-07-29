package postman

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_CreateCollection(t *testing.T) {
	var gotKey, gotMethod, gotPath string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Api-Key")
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"collection": map[string]interface{}{"id": "1", "uid": "u-1", "name": "Test"},
		})
	}))
	defer srv.Close()

	c := NewClient("secret-key")
	c.BaseURL = srv.URL
	resp, err := c.CreateCollection([]byte(`{"info":{"name":"Test"}}`), "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if resp.Collection.UID != "u-1" {
		t.Errorf("UID = %q, want u-1", resp.Collection.UID)
	}
	if gotKey != "secret-key" {
		t.Errorf("X-Api-Key header = %q, want secret-key", gotKey)
	}
	if gotMethod != http.MethodPost || gotPath != "/collections" {
		t.Errorf("request = %s %s, want POST /collections", gotMethod, gotPath)
	}
	if _, ok := gotBody["collection"]; !ok {
		t.Errorf("request body = %v, want the payload wrapped under \"collection\"", gotBody)
	}
}

func TestClient_CreateCollection_WithWorkspace(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"collection": map[string]interface{}{"id": "1", "uid": "u-1", "name": "Test"},
		})
	}))
	defer srv.Close()

	c := NewClient("key")
	c.BaseURL = srv.URL
	if _, err := c.CreateCollection([]byte(`{}`), "ws-1"); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if gotQuery != "workspace=ws-1" {
		t.Errorf("query = %q, want workspace=ws-1", gotQuery)
	}
}

func TestClient_UpdateCollection_InjectsPostmanID(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"collection": map[string]interface{}{"id": "1", "uid": "uid-existing", "name": "Test"},
		})
	}))
	defer srv.Close()

	c := NewClient("secret-key")
	c.BaseURL = srv.URL
	if _, err := c.UpdateCollection("uid-existing", []byte(`{"info":{"name":"Test"}}`)); err != nil {
		t.Fatalf("UpdateCollection: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/collections/uid-existing" {
		t.Errorf("request = %s %s, want PUT /collections/uid-existing", gotMethod, gotPath)
	}
	inner, ok := gotBody["collection"].(map[string]interface{})
	if !ok {
		t.Fatalf("body = %v, want {collection: {...}}", gotBody)
	}
	info, ok := inner["info"].(map[string]interface{})
	if !ok || info["_postman_id"] != "uid-existing" {
		t.Errorf("info._postman_id = %v, want uid-existing", info)
	}
}

func TestClient_RejectsUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient("bad-key")
	c.BaseURL = srv.URL
	if _, err := c.Me(); err == nil {
		t.Fatal("expected an error for a 401 response")
	}
}

func TestClient_NotFoundOnUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient("key")
	c.BaseURL = srv.URL
	if _, err := c.UpdateCollection("gone", []byte(`{}`)); err == nil {
		t.Fatal("expected an error for a 404 response")
	}
}

func TestClient_Me(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me" {
			t.Errorf("path = %q, want /me", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"user": map[string]interface{}{"id": 1, "username": "dev", "email": "dev@example.com"},
		})
	}))
	defer srv.Close()

	c := NewClient("key")
	c.BaseURL = srv.URL
	me, err := c.Me()
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.User.Username != "dev" {
		t.Errorf("Username = %q, want dev", me.User.Username)
	}
}

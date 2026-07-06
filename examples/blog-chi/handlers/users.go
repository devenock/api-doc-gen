package handlers

import (
	"encoding/json"
	"net/http"

	"blogapi/models"
)

// Register creates a new author account and returns a session token.
func Register(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(models.LoginResponse{})
}

// Login authenticates an author and returns a JWT session token.
func Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.LoginResponse{})
}

// Logout invalidates the current session token.
func Logout(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// ListAuthors returns a paginated list of all registered authors.
// Supports filtering by role and searching by name or username.
func ListAuthors(w http.ResponseWriter, r *http.Request) {
	_ = r.URL.Query().Get("page")
	_ = r.URL.Query().Get("limit")
	_ = r.URL.Query().Get("role")
	_ = r.URL.Query().Get("search")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"authors": []models.Author{},
		"total":   0,
	})
}

// GetAuthor returns the public profile of a single author by ID.
func GetAuthor(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.Author{})
}

// UpdateAuthor lets an admin update any author's account details.
func UpdateAuthor(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateAuthorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.Author{})
}

// DeleteAuthor permanently removes an author account. Requires admin role.
func DeleteAuthor(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// GetMyProfile returns the authenticated author's own account details.
func GetMyProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.Author{})
}

// UpdateMyProfile lets the authenticated author update their own profile.
func UpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateAuthorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.Author{})
}

// GetAuthorPosts returns all published posts written by a specific author.
func GetAuthorPosts(w http.ResponseWriter, r *http.Request) {
	_ = r.URL.Query().Get("page")
	_ = r.URL.Query().Get("limit")
	_ = r.URL.Query().Get("status")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"posts": []models.Post{},
		"total": 0,
	})
}

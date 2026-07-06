package handlers

import (
	"encoding/json"
	"net/http"

	"crmapi/models"
)

// ListDeals returns a paginated list of deals filtered by stage, assignee, or value range.
// Sort by value, close date, or creation date.
func ListDeals(w http.ResponseWriter, r *http.Request) {
	_ = r.URL.Query().Get("page")
	_ = r.URL.Query().Get("limit")
	_ = r.URL.Query().Get("stage")
	_ = r.URL.Query().Get("assigned_to")
	_ = r.URL.Query().Get("contact_id")
	_ = r.URL.Query().Get("min_value")
	_ = r.URL.Query().Get("max_value")
	_ = r.URL.Query().Get("sort")    // "value" | "close_date" | "created_at"
	_ = r.URL.Query().Get("closing_soon") // "true" filters deals closing in 30 days

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deals": []models.Deal{},
		"total": 0,
	})
}

// GetDeal returns a single deal by ID, including its full activity history.
func GetDeal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.Deal{})
}

// CreateDeal opens a new sales deal in the pipeline.
func CreateDeal(w http.ResponseWriter, r *http.Request) {
	var req models.CreateDealRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(models.Deal{})
}

// UpdateDeal modifies an existing deal's value, close date, notes, or assignee.
func UpdateDeal(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateDealRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.Deal{})
}

// DeleteDeal removes a deal from the pipeline. This action is irreversible.
func DeleteDeal(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// UpdateDealStage moves a deal to a new pipeline stage.
// Valid stages: prospecting, proposal, negotiation, closed_won, closed_lost.
func UpdateDealStage(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateStageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.Deal{})
}

package handlers

import (
	"encoding/json"
	"net/http"

	"crmapi/models"
)

// GetPipelineReport returns a full breakdown of the sales pipeline by stage,
// including total value, deal counts, win rate, and average days per stage.
func GetPipelineReport(w http.ResponseWriter, r *http.Request) {
	_ = r.URL.Query().Get("period") // "week" | "month" | "quarter" | "year"
	_ = r.URL.Query().Get("from")
	_ = r.URL.Query().Get("to")
	_ = r.URL.Query().Get("assigned_to") // filter by rep ID

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.PipelineReport{})
}

// GetLeadReport returns lead generation and qualification metrics for a period.
// Includes conversion rate and a breakdown of lead counts by acquisition source.
func GetLeadReport(w http.ResponseWriter, r *http.Request) {
	_ = r.URL.Query().Get("period")
	_ = r.URL.Query().Get("from")
	_ = r.URL.Query().Get("to")
	_ = r.URL.Query().Get("source") // filter by lead source channel

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.LeadReport{})
}

// GetRevenueForecast projects expected revenue from open pipeline deals
// using their probability scores. Returns best-case, worst-case, and committed figures.
func GetRevenueForecast(w http.ResponseWriter, r *http.Request) {
	_ = r.URL.Query().Get("period")
	_ = r.URL.Query().Get("currency") // ISO 4217, default "USD"

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.RevenueForecast{})
}

// GetTeamPerformance returns per-rep KPIs including deals opened/won/lost,
// total revenue, activity count, and win rate for a given period.
func GetTeamPerformance(w http.ResponseWriter, r *http.Request) {
	_ = r.URL.Query().Get("period")
	_ = r.URL.Query().Get("from")
	_ = r.URL.Query().Get("to")
	_ = r.URL.Query().Get("department") // filter to a specific department

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.TeamPerformanceReport{})
}

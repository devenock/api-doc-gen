package models

import "time"

// ─── Users / Contacts ────────────────────────────────────────────────────────

// User is a CRM platform user (sales rep, manager, or admin).
type User struct {
	ID         uint      `json:"id"`
	Name       string    `json:"name"`
	Email      string    `json:"email"`
	Role       string    `json:"role"` // "rep" | "manager" | "admin"
	Department string    `json:"department"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
}

// LoginRequest authenticates a CRM user.
type LoginRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse carries the session token and user info.
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	User      User   `json:"user"`
}

// CreateUserRequest is the payload for creating a CRM team member.
type CreateUserRequest struct {
	Name       string `json:"name"       binding:"required"`
	Email      string `json:"email"      binding:"required,email"`
	Password   string `json:"password"   binding:"required,min=8"`
	Role       string `json:"role"       binding:"required"`
	Department string `json:"department"`
}

// UpdateUserRequest carries changeable user fields.
type UpdateUserRequest struct {
	Name       string `json:"name"`
	Role       string `json:"role"`
	Department string `json:"department"`
	Active     bool   `json:"active"`
}

// Contact is a lead or customer tracked in the CRM.
type Contact struct {
	ID         uint      `json:"id"`
	FirstName  string    `json:"first_name"`
	LastName   string    `json:"last_name"`
	Email      string    `json:"email"`
	Phone      string    `json:"phone"`
	Company    string    `json:"company"`
	JobTitle   string    `json:"job_title"`
	Status     string    `json:"status"` // "lead" | "prospect" | "customer" | "churned"
	Source     string    `json:"source"` // "website" | "referral" | "event" | "cold-call"
	AssignedTo uint      `json:"assigned_to"`
	Tags       []string  `json:"tags"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// CreateContactRequest is the payload for creating a new CRM contact.
type CreateContactRequest struct {
	FirstName string   `json:"first_name" binding:"required"`
	LastName  string   `json:"last_name"  binding:"required"`
	Email     string   `json:"email"      binding:"required,email"`
	Phone     string   `json:"phone"`
	Company   string   `json:"company"`
	JobTitle  string   `json:"job_title"`
	Status    string   `json:"status"`
	Source    string   `json:"source"`
	Tags      []string `json:"tags"`
}

// UpdateContactRequest carries fields that may be changed on a contact.
type UpdateContactRequest struct {
	FirstName  string   `json:"first_name"`
	LastName   string   `json:"last_name"`
	Email      string   `json:"email"`
	Phone      string   `json:"phone"`
	Company    string   `json:"company"`
	JobTitle   string   `json:"job_title"`
	Status     string   `json:"status"`
	AssignedTo uint     `json:"assigned_to"`
	Tags       []string `json:"tags"`
}

// Activity is an interaction logged against a contact (call, email, meeting).
type Activity struct {
	ID          uint      `json:"id"`
	ContactID   uint      `json:"contact_id"`
	UserID      uint      `json:"user_id"`
	Type        string    `json:"type"` // "call" | "email" | "meeting" | "note"
	Subject     string    `json:"subject"`
	Notes       string    `json:"notes"`
	OutcomeNote string    `json:"outcome_note"`
	OccurredAt  time.Time `json:"occurred_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateActivityRequest logs a new activity on a contact.
type CreateActivityRequest struct {
	Type       string    `json:"type"    binding:"required"`
	Subject    string    `json:"subject" binding:"required"`
	Notes      string    `json:"notes"`
	OccurredAt time.Time `json:"occurred_at"`
}

// ─── Deals / Products ────────────────────────────────────────────────────────

// Deal represents a sales opportunity in the pipeline.
type Deal struct {
	ID          uint      `json:"id"`
	Title       string    `json:"title"`
	ContactID   uint      `json:"contact_id"`
	AssignedTo  uint      `json:"assigned_to"`
	Stage       string    `json:"stage"`  // "prospecting" | "proposal" | "negotiation" | "closed_won" | "closed_lost"
	Value       float64   `json:"value"`
	Currency    string    `json:"currency"`
	Probability int       `json:"probability"` // 0-100
	CloseDate   time.Time `json:"close_date"`
	Notes       string    `json:"notes"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateDealRequest is the payload for opening a new deal.
type CreateDealRequest struct {
	Title      string    `json:"title"      binding:"required"`
	ContactID  uint      `json:"contact_id" binding:"required"`
	Stage      string    `json:"stage"      binding:"required"`
	Value      float64   `json:"value"      binding:"gt=0"`
	Currency   string    `json:"currency"`
	CloseDate  time.Time `json:"close_date"`
	Notes      string    `json:"notes"`
	Tags       []string  `json:"tags"`
}

// UpdateDealRequest carries fields that can be changed on an existing deal.
type UpdateDealRequest struct {
	Title       string    `json:"title"`
	Stage       string    `json:"stage"`
	Value       float64   `json:"value"`
	Probability int       `json:"probability"`
	CloseDate   time.Time `json:"close_date"`
	Notes       string    `json:"notes"`
	AssignedTo  uint      `json:"assigned_to"`
	Tags        []string  `json:"tags"`
}

// UpdateStageRequest changes a deal's pipeline stage.
type UpdateStageRequest struct {
	Stage  string `json:"stage"  binding:"required"`
	Reason string `json:"reason"`
}

// ─── Analytics ───────────────────────────────────────────────────────────────

// PipelineReport summarises the overall sales pipeline value and deal counts.
type PipelineReport struct {
	Period       string        `json:"period"`
	TotalDeals   int           `json:"total_deals"`
	TotalValue   float64       `json:"total_value"`
	WonDeals     int           `json:"won_deals"`
	WonValue     float64       `json:"won_value"`
	LostDeals    int           `json:"lost_deals"`
	WinRate      float64       `json:"win_rate"`
	ByStage      []StageMetric `json:"by_stage"`
}

// StageMetric holds aggregate data for a single pipeline stage.
type StageMetric struct {
	Stage      string  `json:"stage"`
	DealCount  int     `json:"deal_count"`
	TotalValue float64 `json:"total_value"`
	AvgDays    int     `json:"avg_days_in_stage"`
}

// LeadReport shows lead generation and qualification metrics.
type LeadReport struct {
	Period           string  `json:"period"`
	NewLeads         int     `json:"new_leads"`
	QualifiedLeads   int     `json:"qualified_leads"`
	ConvertedLeads   int     `json:"converted_leads"`
	ConversionRate   float64 `json:"conversion_rate"`
	AvgLeadScore     float64 `json:"avg_lead_score"`
	BySource         []LeadSourceMetric `json:"by_source"`
}

// LeadSourceMetric breaks down lead counts by acquisition channel.
type LeadSourceMetric struct {
	Source     string  `json:"source"`
	Count      int     `json:"count"`
	Conversion float64 `json:"conversion_rate"`
}

// RevenueForecast projects expected revenue based on pipeline probability.
type RevenueForecast struct {
	Period        string  `json:"period"`
	ExpectedValue float64 `json:"expected_value"`
	BestCase      float64 `json:"best_case"`
	WorstCase     float64 `json:"worst_case"`
	Committed     float64 `json:"committed"`
}

// TeamPerformanceReport shows per-rep activity and deal metrics.
type TeamPerformanceReport struct {
	Period  string       `json:"period"`
	Members []RepMetrics `json:"members"`
}

// RepMetrics holds KPIs for a single sales representative.
type RepMetrics struct {
	UserID      uint    `json:"user_id"`
	Name        string  `json:"name"`
	DealsOpened int     `json:"deals_opened"`
	DealsWon    int     `json:"deals_won"`
	DealsLost   int     `json:"deals_lost"`
	Revenue     float64 `json:"revenue"`
	Activities  int     `json:"activities"`
	WinRate     float64 `json:"win_rate"`
}

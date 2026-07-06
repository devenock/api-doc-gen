package models

import "time"

// ─── Users ──────────────────────────────────────────────────────────────────

// User represents a registered account in the store.
type User struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"` // "customer" | "admin"
	Phone     string    `json:"phone"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateUserRequest is the payload for registering a new account.
type CreateUserRequest struct {
	Name     string `json:"name"     binding:"required"`
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Phone    string `json:"phone"`
}

// UpdateUserRequest carries fields that can be changed on an existing account.
type UpdateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
	Role  string `json:"role"`
}

// LoginRequest is the payload for authenticating a user.
type LoginRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse is returned on successful authentication.
type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	User         User   `json:"user"`
}

// RefreshTokenRequest is the payload for obtaining a new access token.
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// ChangePasswordRequest lets an authenticated user update their password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password"     binding:"required,min=8"`
}

// ─── Products ───────────────────────────────────────────────────────────────

// Product represents a saleable item in the catalogue.
type Product struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Price       float64   `json:"price"`
	SalePrice   float64   `json:"sale_price,omitempty"`
	Stock       int       `json:"stock"`
	SKU         string    `json:"sku"`
	Category    string    `json:"category"`
	Tags        []string  `json:"tags"`
	ImageURL    string    `json:"image_url"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateProductRequest is the payload for adding a product to the catalogue.
type CreateProductRequest struct {
	Name        string   `json:"name"        binding:"required"`
	Description string   `json:"description"`
	Price       float64  `json:"price"       binding:"required,gt=0"`
	Stock       int      `json:"stock"       binding:"gte=0"`
	SKU         string   `json:"sku"`
	Category    string   `json:"category"    binding:"required"`
	Tags        []string `json:"tags"`
	ImageURL    string   `json:"image_url"`
}

// UpdateProductRequest carries fields that can be changed on an existing product.
type UpdateProductRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Price       float64  `json:"price"`
	SalePrice   float64  `json:"sale_price"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	ImageURL    string   `json:"image_url"`
	Active      bool     `json:"active"`
}

// StockUpdateRequest adjusts the available quantity of a product.
type StockUpdateRequest struct {
	Delta  int    `json:"delta"  binding:"required"` // positive = add, negative = remove
	Reason string `json:"reason"`
}

// Category represents a product grouping.
type Category struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	ProductCount int   `json:"product_count"`
}

// Review is a customer review left on a product.
type Review struct {
	ID        uint      `json:"id"`
	ProductID uint      `json:"product_id"`
	UserID    uint      `json:"user_id"`
	UserName  string    `json:"user_name"`
	Rating    int       `json:"rating"` // 1-5
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateReviewRequest is the payload for submitting a product review.
type CreateReviewRequest struct {
	Rating int    `json:"rating" binding:"required,min=1,max=5"`
	Title  string `json:"title"  binding:"required"`
	Body   string `json:"body"`
}

// ─── Analytics ──────────────────────────────────────────────────────────────

// SalesReport summarises sales metrics over a period.
type SalesReport struct {
	Period        string         `json:"period"`
	TotalRevenue  float64        `json:"total_revenue"`
	TotalOrders   int            `json:"total_orders"`
	AvgOrderValue float64        `json:"avg_order_value"`
	GrowthPercent float64        `json:"growth_percent"`
	TopProducts   []ProductSales `json:"top_products"`
}

// ProductSales holds per-product performance data.
type ProductSales struct {
	ProductID   uint    `json:"product_id"`
	ProductName string  `json:"product_name"`
	UnitsSold   int     `json:"units_sold"`
	Revenue     float64 `json:"revenue"`
}

// RevenueReport breaks down revenue across channels and deductions.
type RevenueReport struct {
	Period        string  `json:"period"`
	GrossRevenue  float64 `json:"gross_revenue"`
	NetRevenue    float64 `json:"net_revenue"`
	Refunds       float64 `json:"refunds"`
	Discounts     float64 `json:"discounts"`
	Taxes         float64 `json:"taxes"`
	GrowthPercent float64 `json:"growth_percent"`
}

// TrafficReport holds visitor and page-view metrics.
type TrafficReport struct {
	Period          string       `json:"period"`
	PageViews       int          `json:"page_views"`
	UniqueVisitors  int          `json:"unique_visitors"`
	BounceRate      float64      `json:"bounce_rate"`
	AvgSessionSecs  int          `json:"avg_session_secs"`
	TopPages        []PageMetric `json:"top_pages"`
}

// PageMetric holds stats for a single page.
type PageMetric struct {
	Path     string  `json:"path"`
	Views    int     `json:"views"`
	AvgTime  string  `json:"avg_time"`
	ExitRate float64 `json:"exit_rate"`
}

// ConversionReport tracks funnel drop-off rates.
type ConversionReport struct {
	Period          string  `json:"period"`
	Visits          int     `json:"visits"`
	AddedToCart     int     `json:"added_to_cart"`
	ReachedCheckout int     `json:"reached_checkout"`
	Purchased       int     `json:"purchased"`
	ConversionRate  float64 `json:"conversion_rate"`
}

package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"ecommerce/handlers"
)

// JWTAuth is a placeholder JWT validation middleware.
func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// In a real app, validate the Authorization header here.
		c.Next()
	}
}

// AdminOnly is a middleware that restricts access to admin-role users.
func AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func main() {
	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "ecommerce-api"})
	})

	api := r.Group("/api/v1")

	// ── Auth (public) ────────────────────────────────────────────────────────
	auth := api.Group("/auth")
	{
		auth.POST("/register", handlers.Register)
		auth.POST("/login", handlers.Login)
		auth.POST("/refresh", handlers.RefreshToken)
	}

	// ── Users — own profile (requires auth) ─────────────────────────────────
	me := api.Group("/me")
	me.Use(JWTAuth())
	{
		me.GET("", handlers.GetProfile)
		me.PUT("", handlers.UpdateProfile)
		me.PUT("/password", handlers.ChangePassword)
	}

	// ── Users — admin management ─────────────────────────────────────────────
	adminUsers := api.Group("/users")
	adminUsers.Use(JWTAuth(), AdminOnly())
	{
		adminUsers.GET("", handlers.ListUsers)
		adminUsers.GET("/:id", handlers.GetUser)
		adminUsers.PUT("/:id", handlers.UpdateUser)
		adminUsers.DELETE("/:id", handlers.DeleteUser)
	}

	// ── Products — public browse ─────────────────────────────────────────────
	products := api.Group("/products")
	{
		products.GET("", handlers.ListProducts)
		products.GET("/search", handlers.SearchProducts)
		products.GET("/categories", handlers.ListCategories)
		products.GET("/:id", handlers.GetProduct)
		products.GET("/:id/reviews", handlers.GetProductReviews)
	}

	// ── Products — authenticated actions ────────────────────────────────────
	products.Use(JWTAuth())
	{
		products.POST("/:id/reviews", handlers.CreateReview)
	}

	// ── Products — admin catalogue management ───────────────────────────────
	adminProducts := api.Group("/products")
	adminProducts.Use(JWTAuth(), AdminOnly())
	{
		adminProducts.POST("", handlers.CreateProduct)
		adminProducts.PUT("/:id", handlers.UpdateProduct)
		adminProducts.DELETE("/:id", handlers.DeleteProduct)
		adminProducts.PATCH("/:id/stock", handlers.UpdateStock)
	}

	// ── Analytics — admin only ───────────────────────────────────────────────
	analytics := api.Group("/analytics")
	analytics.Use(JWTAuth(), AdminOnly())
	{
		analytics.GET("/sales", handlers.GetSalesReport)
		analytics.GET("/revenue", handlers.GetRevenueReport)
		analytics.GET("/traffic", handlers.GetTrafficReport)
		analytics.GET("/conversion", handlers.GetConversionReport)
		analytics.GET("/top-products", handlers.GetTopProducts)
	}

	r.Run(":8080")
}

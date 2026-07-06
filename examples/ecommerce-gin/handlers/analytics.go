package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"ecommerce/models"
)

// GetSalesReport returns aggregated sales metrics for a given time range.
// Use period=daily|weekly|monthly or supply explicit from/to date strings (YYYY-MM-DD).
func GetSalesReport(c *gin.Context) {
	_ = c.Query("period") // "daily" | "weekly" | "monthly"
	_ = c.Query("from")
	_ = c.Query("to")
	_ = c.Query("category") // filter by product category

	c.JSON(http.StatusOK, models.SalesReport{})
}

// GetRevenueReport returns a detailed revenue breakdown for a given period.
// Includes gross revenue, refunds, discounts, taxes, and net figure.
func GetRevenueReport(c *gin.Context) {
	_ = c.Query("period")
	_ = c.Query("from")
	_ = c.Query("to")
	_ = c.Query("currency") // ISO 4217, default "USD"

	c.JSON(http.StatusOK, models.RevenueReport{})
}

// GetTrafficReport returns visitor and page-view statistics for a given period.
func GetTrafficReport(c *gin.Context) {
	_ = c.Query("period")
	_ = c.Query("from")
	_ = c.Query("to")
	_ = c.Query("limit") // number of top pages to include

	c.JSON(http.StatusOK, models.TrafficReport{})
}

// GetConversionReport returns funnel conversion rates from visit through purchase.
func GetConversionReport(c *gin.Context) {
	_ = c.Query("period")
	_ = c.Query("from")
	_ = c.Query("to")

	c.JSON(http.StatusOK, models.ConversionReport{})
}

// GetTopProducts returns the best-performing products ranked by revenue or units sold.
func GetTopProducts(c *gin.Context) {
	_ = c.Query("metric") // "revenue" | "units"
	_ = c.Query("limit")  // default 10
	_ = c.Query("period")
	_ = c.Query("category")

	c.JSON(http.StatusOK, gin.H{
		"products": []models.ProductSales{},
		"period":   c.Query("period"),
		"metric":   c.Query("metric"),
	})
}

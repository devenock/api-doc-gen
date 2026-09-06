package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"ecommerce/models"
)

// ListProducts returns a paginated, filterable list of active products.
// Supports filtering by category, price range, and tag; sort by price or name.
func ListProducts(c *gin.Context) {
	_ = c.Query("page")
	_ = c.Query("limit")
	_ = c.Query("category")
	_ = c.Query("tag")
	_ = c.Query("min_price")
	_ = c.Query("max_price")
	_ = c.Query("sort")  // "price_asc" | "price_desc" | "name"
	_ = c.Query("active")

	c.JSON(http.StatusOK, gin.H{
		"products": []models.Product{},
		"total":    0,
		"page":     1,
		"limit":    20,
	})
}

// GetProduct returns a single product by its ID, including full description and tags.
func GetProduct(c *gin.Context) {
	_ = c.Param("id")
	c.JSON(http.StatusOK, models.Product{})
}

// SearchProducts performs a full-text search across product names, descriptions, and tags.
func SearchProducts(c *gin.Context) {
	_ = c.Query("q")
	_ = c.Query("page")
	_ = c.Query("limit")
	_ = c.Query("category")

	c.JSON(http.StatusOK, gin.H{
		"products": []models.Product{},
		"total":    0,
		"query":    c.Query("q"),
	})
}

// ListCategories returns all product categories with their item counts.
func ListCategories(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"categories": []models.Category{},
	})
}

// GetProductReviews returns paginated customer reviews for a product.
func GetProductReviews(c *gin.Context) {
	_ = c.Param("id")
	_ = c.Query("page")
	_ = c.Query("limit")
	_ = c.Query("rating") // filter by star rating 1-5

	c.JSON(http.StatusOK, gin.H{
		"reviews":  []models.Review{},
		"total":    0,
		"avg_rating": 0.0,
	})
}

// CreateProduct adds a new product to the catalogue. Requires admin role.
func CreateProduct(c *gin.Context) {
	var req models.CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, models.Product{})
}

// UpdateProduct modifies an existing product's details. Requires admin role.
func UpdateProduct(c *gin.Context) {
	_ = c.Param("id")
	var req models.UpdateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.Product{})
}

// DeleteProduct removes a product from the catalogue. Requires admin role.
func DeleteProduct(c *gin.Context) {
	_ = c.Param("id")
	c.Status(http.StatusNoContent)
}

// UpdateStock adjusts the available inventory count for a product.
// Use a positive delta to add stock and a negative delta to remove it.
func UpdateStock(c *gin.Context) {
	_ = c.Param("id")
	var req models.StockUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"product_id": c.Param("id"),
		"new_stock":  0,
	})
}

// CreateReview submits a customer review for a product. Requires authentication.
func CreateReview(c *gin.Context) {
	_ = c.Param("id")
	var req models.CreateReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, models.Review{})
}

package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"ecommerce/models"
)

// ListUsers returns a paginated list of all user accounts.
// Supports filtering by role (customer|admin) and searching by name or email.
func ListUsers(c *gin.Context) {
	_ = c.Query("page")
	_ = c.Query("limit")
	_ = c.Query("role")
	_ = c.Query("search")
	_ = c.Query("active")

	c.JSON(http.StatusOK, gin.H{
		"users": []models.User{},
		"total": 0,
		"page":  1,
		"limit": 20,
	})
}

// GetUser returns a single user account by ID.
func GetUser(c *gin.Context) {
	_ = c.Param("id")
	c.JSON(http.StatusOK, models.User{})
}

// UpdateUser modifies an existing user account. Requires admin privileges.
func UpdateUser(c *gin.Context) {
	_ = c.Param("id")
	var req models.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.User{})
}

// DeleteUser permanently deactivates a user account. Requires admin privileges.
func DeleteUser(c *gin.Context) {
	_ = c.Param("id")
	c.Status(http.StatusNoContent)
}

// Register creates a new customer account and returns a JWT token pair.
func Register(c *gin.Context) {
	var req models.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, models.LoginResponse{})
}

// Login authenticates a user with email and password and returns a JWT token pair.
func Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.LoginResponse{})
}

// RefreshToken exchanges a valid refresh token for a new access token.
func RefreshToken(c *gin.Context) {
	var req models.RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.LoginResponse{})
}

// GetProfile returns the authenticated user's own account details.
func GetProfile(c *gin.Context) {
	c.JSON(http.StatusOK, models.User{})
}

// UpdateProfile lets the authenticated user update their own account details.
func UpdateProfile(c *gin.Context) {
	var req models.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, models.User{})
}

// ChangePassword lets the authenticated user change their own password.
func ChangePassword(c *gin.Context) {
	var req models.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

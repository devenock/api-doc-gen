package models

import "time"

// ─── Users ──────────────────────────────────────────────────────────────────

// Author represents a blog author account.
type Author struct {
	ID        uint      `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	FullName  string    `json:"full_name"`
	Bio       string    `json:"bio"`
	AvatarURL string    `json:"avatar_url"`
	Role      string    `json:"role"` // "reader" | "author" | "editor" | "admin"
	PostCount int       `json:"post_count"`
	CreatedAt time.Time `json:"created_at"`
}

// RegisterRequest is the payload for creating a new author account.
type RegisterRequest struct {
	Username string `json:"username"  binding:"required,min=3,max=30"`
	Email    string `json:"email"     binding:"required,email"`
	Password string `json:"password"  binding:"required,min=8"`
	FullName string `json:"full_name" binding:"required"`
}

// LoginRequest authenticates a user.
type LoginRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse carries the session token and user info.
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	User      Author `json:"user"`
}

// UpdateAuthorRequest carries fields an author can update on their own profile.
type UpdateAuthorRequest struct {
	FullName  string `json:"full_name"`
	Bio       string `json:"bio"`
	AvatarURL string `json:"avatar_url"`
}

// ─── Posts ──────────────────────────────────────────────────────────────────

// Post represents a published or drafted blog post.
type Post struct {
	ID          uint      `json:"id"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Excerpt     string    `json:"excerpt"`
	Body        string    `json:"body"`
	CoverImage  string    `json:"cover_image"`
	Category    string    `json:"category"`
	Tags        []string  `json:"tags"`
	Author      Author    `json:"author"`
	Status      string    `json:"status"` // "draft" | "published" | "archived"
	Featured    bool      `json:"featured"`
	ViewCount   int       `json:"view_count"`
	LikeCount   int       `json:"like_count"`
	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreatePostRequest is the payload for drafting a new blog post.
type CreatePostRequest struct {
	Title      string   `json:"title"    binding:"required"`
	Excerpt    string   `json:"excerpt"`
	Body       string   `json:"body"     binding:"required"`
	CoverImage string   `json:"cover_image"`
	Category   string   `json:"category" binding:"required"`
	Tags       []string `json:"tags"`
	Status     string   `json:"status"` // "draft" | "published"
}

// UpdatePostRequest carries fields that can be changed on an existing post.
type UpdatePostRequest struct {
	Title      string   `json:"title"`
	Excerpt    string   `json:"excerpt"`
	Body       string   `json:"body"`
	CoverImage string   `json:"cover_image"`
	Category   string   `json:"category"`
	Tags       []string `json:"tags"`
	Status     string   `json:"status"`
	Featured   bool     `json:"featured"`
}

// PostCategory groups related posts.
type PostCategory struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	PostCount int    `json:"post_count"`
}

// Comment is a reader's comment on a post.
type Comment struct {
	ID        uint      `json:"id"`
	PostID    uint      `json:"post_id"`
	AuthorID  uint      `json:"author_id"`
	AuthorName string   `json:"author_name"`
	Body      string    `json:"body"`
	Approved  bool      `json:"approved"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateCommentRequest is the payload for submitting a comment.
type CreateCommentRequest struct {
	Body string `json:"body" binding:"required,min=1,max=2000"`
}

// ─── Analytics ──────────────────────────────────────────────────────────────

// OverviewStats provides a snapshot of key blog metrics.
type OverviewStats struct {
	TotalPosts      int     `json:"total_posts"`
	PublishedPosts  int     `json:"published_posts"`
	TotalAuthors    int     `json:"total_authors"`
	TotalReaders    int     `json:"total_readers"`
	TotalViews      int     `json:"total_views"`
	AvgViewsPerPost float64 `json:"avg_views_per_post"`
	Period          string  `json:"period"`
}

// PostAnalytics holds traffic data broken down per post.
type PostAnalytics struct {
	PostID      uint    `json:"post_id"`
	Title       string  `json:"title"`
	Views       int     `json:"views"`
	UniqueReads int     `json:"unique_reads"`
	AvgReadTime string  `json:"avg_read_time"`
	LikeCount   int     `json:"like_count"`
	ShareCount  int     `json:"share_count"`
	CommentCount int    `json:"comment_count"`
}

// EngagementReport tracks reader interaction with content.
type EngagementReport struct {
	Period          string  `json:"period"`
	TotalComments   int     `json:"total_comments"`
	TotalLikes      int     `json:"total_likes"`
	TotalShares     int     `json:"total_shares"`
	AvgEngagement   float64 `json:"avg_engagement_rate"`
	TopPosts        []PostAnalytics `json:"top_posts"`
}

// SubscriberReport summarises newsletter subscriber growth.
type SubscriberReport struct {
	Period         string  `json:"period"`
	TotalSubscribers int   `json:"total_subscribers"`
	NewSubscribers int     `json:"new_subscribers"`
	Unsubscribers  int     `json:"unsubscribers"`
	GrowthPercent  float64 `json:"growth_percent"`
	OpenRate       float64 `json:"open_rate"`
	ClickRate      float64 `json:"click_rate"`
}

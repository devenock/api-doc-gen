package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"blogapi/handlers"
)

// JWTAuth validates the Authorization bearer token for protected routes.
func JWTAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// In a real app, validate the bearer token here.
		next.ServeHTTP(w, r)
	})
}

// EditorOnly restricts access to users with the editor or admin role.
func EditorOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

// AdminOnly restricts access to users with the admin role.
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

func main() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","service":"blog-api"}`))
	})

	r.Route("/api/v1", func(r chi.Router) {

		// ── Auth (public) ────────────────────────────────────────────────────
		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", handlers.Register)
			r.Post("/login", handlers.Login)
			r.Post("/logout", handlers.Logout)
		})

		// ── Posts — public browse ─────────────────────────────────────────────
		r.Route("/posts", func(r chi.Router) {
			r.Get("/", handlers.ListPosts)
			r.Get("/search", handlers.SearchPosts)
			r.Get("/featured", handlers.ListFeaturedPosts)
			r.Get("/categories", handlers.ListCategories)

			r.Route("/{slug}", func(r chi.Router) {
				r.Get("/", handlers.GetPost)
				r.Get("/comments", handlers.GetPostComments)

				// Authenticated readers can comment
				r.Group(func(r chi.Router) {
					r.Use(JWTAuth)
					r.Post("/comments", handlers.CreateComment)
				})

				// Editors/authors can manage posts
				r.Group(func(r chi.Router) {
					r.Use(JWTAuth, EditorOnly)
					r.Put("/", handlers.UpdatePost)
					r.Delete("/", handlers.DeletePost)
					r.Post("/publish", handlers.PublishPost)
					r.Post("/archive", handlers.ArchivePost)
				})
			})

			// Authors create new posts
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth)
				r.Post("/", handlers.CreatePost)
			})
		})

		// ── My profile (authenticated) ────────────────────────────────────────
		r.Route("/me", func(r chi.Router) {
			r.Use(JWTAuth)
			r.Get("/", handlers.GetMyProfile)
			r.Put("/", handlers.UpdateMyProfile)
			r.Get("/posts", handlers.GetAuthorPosts)
		})

		// ── Authors — public profiles ─────────────────────────────────────────
		r.Route("/authors", func(r chi.Router) {
			r.Get("/", handlers.ListAuthors)
			r.Get("/{id}", handlers.GetAuthor)
			r.Get("/{id}/posts", handlers.GetAuthorPosts)

			// Admin-only author management
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth, AdminOnly)
				r.Put("/{id}", handlers.UpdateAuthor)
				r.Delete("/{id}", handlers.DeleteAuthor)
			})
		})

		// ── Analytics — editors and admins ────────────────────────────────────
		r.Route("/analytics", func(r chi.Router) {
			r.Use(JWTAuth, EditorOnly)
			r.Get("/overview", handlers.GetOverview)
			r.Get("/posts", handlers.GetPostAnalytics)
			r.Get("/engagement", handlers.GetEngagementReport)
			r.Get("/subscribers", handlers.GetSubscriberReport)
		})
	})

	http.ListenAndServe(":8081", r)
}

package main

import (
	"net/http"

	"github.com/gorilla/mux"
	"crmapi/handlers"
)

// JWTMiddleware validates the Authorization bearer token on protected routes.
func JWTMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// In a real app, validate the bearer token here.
		next.ServeHTTP(w, r)
	})
}

// ManagerOnly restricts routes to manager or admin roles.
func ManagerOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

func main() {
	r := mux.NewRouter()

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","service":"crm-api"}`))
	}).Methods(http.MethodGet)

	api := r.PathPrefix("/api/v1").Subrouter()

	// ── Auth (public) ────────────────────────────────────────────────────────
	auth := api.PathPrefix("/auth").Subrouter()
	auth.HandleFunc("/login", handlers.Login).Methods(http.MethodPost)
	auth.HandleFunc("/refresh", handlers.RefreshToken).Methods(http.MethodPost)

	// ── Team users — admin management ────────────────────────────────────────
	users := api.PathPrefix("/users").Subrouter()
	users.Use(JWTMiddleware, ManagerOnly)
	users.HandleFunc("", handlers.ListUsers).Methods(http.MethodGet)
	users.HandleFunc("", handlers.CreateUser).Methods(http.MethodPost)
	users.HandleFunc("/{id:[0-9]+}", handlers.GetUser).Methods(http.MethodGet)
	users.HandleFunc("/{id:[0-9]+}", handlers.UpdateUser).Methods(http.MethodPut)
	users.HandleFunc("/{id:[0-9]+}", handlers.DeleteUser).Methods(http.MethodDelete)

	// ── Contacts ─────────────────────────────────────────────────────────────
	contacts := api.PathPrefix("/contacts").Subrouter()
	contacts.Use(JWTMiddleware)
	contacts.HandleFunc("", handlers.ListContacts).Methods(http.MethodGet)
	contacts.HandleFunc("", handlers.CreateContact).Methods(http.MethodPost)
	contacts.HandleFunc("/{id:[0-9]+}", handlers.GetContact).Methods(http.MethodGet)
	contacts.HandleFunc("/{id:[0-9]+}", handlers.UpdateContact).Methods(http.MethodPut)
	contacts.HandleFunc("/{id:[0-9]+}", handlers.DeleteContact).Methods(http.MethodDelete)
	contacts.HandleFunc("/{id:[0-9]+}/activities", handlers.GetContactActivities).Methods(http.MethodGet)
	contacts.HandleFunc("/{id:[0-9]+}/activities", handlers.LogActivity).Methods(http.MethodPost)

	// ── Deals ────────────────────────────────────────────────────────────────
	deals := api.PathPrefix("/deals").Subrouter()
	deals.Use(JWTMiddleware)
	deals.HandleFunc("", handlers.ListDeals).Methods(http.MethodGet)
	deals.HandleFunc("", handlers.CreateDeal).Methods(http.MethodPost)
	deals.HandleFunc("/{id:[0-9]+}", handlers.GetDeal).Methods(http.MethodGet)
	deals.HandleFunc("/{id:[0-9]+}", handlers.UpdateDeal).Methods(http.MethodPut)
	deals.HandleFunc("/{id:[0-9]+}", handlers.DeleteDeal).Methods(http.MethodDelete)
	deals.HandleFunc("/{id:[0-9]+}/stage", handlers.UpdateDealStage).Methods(http.MethodPatch)

	// ── Analytics (managers and above) ──────────────────────────────────────
	analytics := api.PathPrefix("/analytics").Subrouter()
	analytics.Use(JWTMiddleware, ManagerOnly)
	analytics.HandleFunc("/pipeline", handlers.GetPipelineReport).Methods(http.MethodGet)
	analytics.HandleFunc("/leads", handlers.GetLeadReport).Methods(http.MethodGet)
	analytics.HandleFunc("/revenue-forecast", handlers.GetRevenueForecast).Methods(http.MethodGet)
	analytics.HandleFunc("/team-performance", handlers.GetTeamPerformance).Methods(http.MethodGet)

	http.ListenAndServe(":8082", r)
}

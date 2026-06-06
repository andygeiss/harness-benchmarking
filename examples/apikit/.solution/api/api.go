package api

import (
	"encoding/json"
	"net/http"

	"apikit/health"
	"apikit/notes"
	"apikit/tasks"
	"apikit/users"
)

func New() http.Handler {
	// Create each feature handler once (each has its own independent store).
	hHealth := health.New()
	hUsers := users.New()
	hTasks := tasks.New()
	hNotes := notes.New()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		hHealth.ServeHTTP(w, r)
	})
	mux.HandleFunc("POST /users", func(w http.ResponseWriter, r *http.Request) {
		hUsers.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /users", func(w http.ResponseWriter, r *http.Request) {
		hUsers.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		hUsers.ServeHTTP(w, r)
	})
	mux.HandleFunc("PUT /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		hUsers.ServeHTTP(w, r)
	})
	mux.HandleFunc("DELETE /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		hUsers.ServeHTTP(w, r)
	})
	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
		hTasks.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		hTasks.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		hTasks.ServeHTTP(w, r)
	})
	mux.HandleFunc("PUT /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		hTasks.ServeHTTP(w, r)
	})
	mux.HandleFunc("DELETE /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		hTasks.ServeHTTP(w, r)
	})
	mux.HandleFunc("POST /notes", func(w http.ResponseWriter, r *http.Request) {
		hNotes.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /notes", func(w http.ResponseWriter, r *http.Request) {
		hNotes.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		hNotes.ServeHTTP(w, r)
	})
	mux.HandleFunc("PUT /notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		hNotes.ServeHTTP(w, r)
	})
	mux.HandleFunc("DELETE /notes/{id}", func(w http.ResponseWriter, r *http.Request) {
		hNotes.ServeHTTP(w, r)
	})

	// Catch-all 404
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	})

	return mux
}

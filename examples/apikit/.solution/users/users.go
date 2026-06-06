package users

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// User is the resource type exposed by the API.
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type store struct {
	mu      sync.Mutex
	nextID  int
	byID    map[int]*User
	byEmail map[string]int // email -> id
	order   []int          // insertion-order of ids
}

func New() http.Handler {
	s := &store{
		byID:    make(map[int]*User),
		byEmail: make(map[string]int),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /users", s.handleCreate)
	mux.HandleFunc("GET /users", s.handleList)
	mux.HandleFunc("GET /users/{id}", s.handleGet)
	mux.HandleFunc("PUT /users/{id}", s.handleUpdate)
	mux.HandleFunc("DELETE /users/{id}", s.handleDelete)
	return mux
}

func (s *store) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Email == "" {
		http.Error(w, `{"error":"name and email required"}`, http.StatusBadRequest)
		return
	}
	if !strings.Contains(req.Email, "@") {
		http.Error(w, `{"error":"email must contain @"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	// check duplicate email
	if _, ok := s.byEmail[req.Email]; ok {
		s.mu.Unlock()
		http.Error(w, `{"error":"email already in use"}`, http.StatusConflict)
		return
	}
	s.nextID++
	u := &User{ID: s.nextID, Name: req.Name, Email: req.Email}
	s.byID[u.ID] = u
	s.byEmail[req.Email] = u.ID
	s.order = append(s.order, u.ID)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(u)
}

func (s *store) handleList(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	result := make([]*User, 0, len(s.order))
	for _, id := range s.order {
		result = append(result, s.byID[id])
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *store) handleGet(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.URL.Path, "/users/")
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	u, ok := s.byID[id]
	s.mu.Unlock()
	if !ok {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(u)
}

func (s *store) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.URL.Path, "/users/")
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	var req struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Email == "" {
		http.Error(w, `{"error":"name and email required"}`, http.StatusBadRequest)
		return
	}
	if !strings.Contains(req.Email, "@") {
		http.Error(w, `{"error":"email must contain @"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	u, ok := s.byID[id]
	if !ok {
		s.mu.Unlock()
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	// check email uniqueness against other users
	if oldEmailID, ok2 := s.byEmail[req.Email]; ok2 && oldEmailID != id {
		s.mu.Unlock()
		http.Error(w, `{"error":"email already in use"}`, http.StatusConflict)
		return
	}
	// remove old email mapping
	delete(s.byEmail, u.Email)
	u.Name = req.Name
	u.Email = req.Email
	s.byEmail[req.Email] = id
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(u)
}

func (s *store) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.URL.Path, "/users/")
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	u, ok := s.byID[id]
	if !ok {
		s.mu.Unlock()
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	delete(s.byEmail, u.Email)
	delete(s.byID, id)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// parseID extracts the integer id from a path like "/users/{id}".
func parseID(path, prefix string) (int, error) {
	s := strings.TrimPrefix(path, prefix)
	id, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid id: %w", err)
	}
	return id, nil
}

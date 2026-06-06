package notes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Note is the resource type exposed by the API.
type Note struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

type store struct {
	mu     sync.Mutex
	nextID int
	byID   map[int]*Note
	order  []int
}

func New() http.Handler {
	s := &store{
		byID: make(map[int]*Note),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /notes", s.handleCreate)
	mux.HandleFunc("GET /notes", s.handleList)
	mux.HandleFunc("GET /notes/{id}", s.handleGet)
	mux.HandleFunc("PUT /notes/{id}", s.handleUpdate)
	mux.HandleFunc("DELETE /notes/{id}", s.handleDelete)
	return mux
}

func (s *store) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		http.Error(w, `{"error":"title required"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.nextID++
	n := &Note{ID: s.nextID, Title: req.Title, Body: req.Body}
	s.byID[n.ID] = n
	s.order = append(s.order, n.ID)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(n)
}

func (s *store) handleList(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	result := make([]*Note, 0, len(s.order))
	for _, id := range s.order {
		result = append(result, s.byID[id])
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *store) handleGet(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.URL.Path, "/notes/")
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	n, ok := s.byID[id]
	s.mu.Unlock()
	if !ok {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(n)
}

func (s *store) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.URL.Path, "/notes/")
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		http.Error(w, `{"error":"title required"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	n, ok := s.byID[id]
	if !ok {
		s.mu.Unlock()
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	n.Title = req.Title
	n.Body = req.Body
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(n)
}

func (s *store) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.URL.Path, "/notes/")
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	_, ok := s.byID[id]
	if !ok {
		s.mu.Unlock()
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	delete(s.byID, id)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func parseID(path, prefix string) (int, error) {
	s := strings.TrimPrefix(path, prefix)
	id, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid id: %w", err)
	}
	return id, nil
}

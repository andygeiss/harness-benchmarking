package tasks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Task is the resource type exposed by the API.
type Task struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

type store struct {
	mu     sync.Mutex
	nextID int
	byID   map[int]*Task
	order  []int
}

func New() http.Handler {
	s := &store{
		byID: make(map[int]*Task),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tasks", s.handleCreate)
	mux.HandleFunc("GET /tasks", s.handleList)
	mux.HandleFunc("GET /tasks/{id}", s.handleGet)
	mux.HandleFunc("PUT /tasks/{id}", s.handleUpdate)
	mux.HandleFunc("DELETE /tasks/{id}", s.handleDelete)
	return mux
}

func (s *store) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Done  *bool  `json:"done"`
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
	t := &Task{ID: s.nextID, Title: req.Title, Done: false}
	if req.Done != nil {
		t.Done = *req.Done
	}
	s.byID[t.ID] = t
	s.order = append(s.order, t.ID)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

func (s *store) handleList(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	result := make([]*Task, 0, len(s.order))
	for _, id := range s.order {
		result = append(result, s.byID[id])
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *store) handleGet(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.URL.Path, "/tasks/")
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	t, ok := s.byID[id]
	s.mu.Unlock()
	if !ok {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

func (s *store) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.URL.Path, "/tasks/")
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}
	var req struct {
		Title string `json:"title"`
		Done  *bool  `json:"done"`
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
	t, ok := s.byID[id]
	if !ok {
		s.mu.Unlock()
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	t.Title = req.Title
	if req.Done != nil {
		t.Done = *req.Done
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

func (s *store) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.URL.Path, "/tasks/")
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

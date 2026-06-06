package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthMounted(t *testing.T) {
	h := New()
	rec := do(t, h, http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", rec.Code)
	}
}

func TestUsersMountedRoundTrip(t *testing.T) {
	h := New()
	rec := do(t, h, http.MethodPost, "/users", `{"name":"Ann","email":"ann@x.io"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /users = %d, want 201", rec.Code)
	}
	var created struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("create body: %v", err)
	}
	if created.ID != 1 {
		t.Fatalf("created id = %d, want 1", created.ID)
	}
	if rec := do(t, h, http.MethodGet, "/users/1", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /users/1 = %d, want 200", rec.Code)
	}
}

func TestTasksAndNotesMounted(t *testing.T) {
	h := New()
	if rec := do(t, h, http.MethodPost, "/tasks", `{"title":"x"}`); rec.Code != http.StatusCreated {
		t.Errorf("POST /tasks = %d, want 201", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, "/notes", `{"title":"n","body":"b"}`); rec.Code != http.StatusCreated {
		t.Errorf("POST /notes = %d, want 201", rec.Code)
	}
}

func TestUnknownPathNotFound(t *testing.T) {
	h := New()
	rec := do(t, h, http.MethodGet, "/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /nope = %d, want 404", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("404 body not JSON: %v", err)
	}
	if body["error"] != "not found" {
		t.Errorf("404 error = %q, want %q", body["error"], "not found")
	}
}

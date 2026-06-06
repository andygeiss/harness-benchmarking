package notes

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

func TestCreateAndGet(t *testing.T) {
	h := New()
	rec := do(t, h, http.MethodPost, "/notes", `{"title":"groceries","body":"milk, eggs"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", rec.Code)
	}
	var n Note
	if err := json.Unmarshal(rec.Body.Bytes(), &n); err != nil {
		t.Fatalf("create body: %v", err)
	}
	if n.ID != 1 || n.Title != "groceries" || n.Body != "milk, eggs" {
		t.Fatalf("created = %+v", n)
	}

	rec = do(t, h, http.MethodGet, "/notes/1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, want 200", rec.Code)
	}
	var got Note
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("get body: %v", err)
	}
	if got != n {
		t.Errorf("get = %+v, want %+v", got, n)
	}
}

func TestCreateRequiresTitleBodyOptional(t *testing.T) {
	h := New()
	// empty title is rejected
	if rec := do(t, h, http.MethodPost, "/notes", `{"title":"","body":"x"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty title = %d, want 400", rec.Code)
	}
	// empty body is fine
	if rec := do(t, h, http.MethodPost, "/notes", `{"title":"t"}`); rec.Code != http.StatusCreated {
		t.Errorf("empty body = %d, want 201", rec.Code)
	}
}

func TestListInsertionOrder(t *testing.T) {
	h := New()
	do(t, h, http.MethodPost, "/notes", `{"title":"A","body":""}`)
	do(t, h, http.MethodPost, "/notes", `{"title":"B","body":""}`)
	rec := do(t, h, http.MethodGet, "/notes", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200", rec.Code)
	}
	var ns []Note
	if err := json.Unmarshal(rec.Body.Bytes(), &ns); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(ns) != 2 || ns[0].Title != "A" || ns[1].Title != "B" {
		t.Errorf("list = %+v, want [A, B] in order", ns)
	}
}

func TestUpdate(t *testing.T) {
	h := New()
	do(t, h, http.MethodPost, "/notes", `{"title":"A","body":"x"}`)
	rec := do(t, h, http.MethodPut, "/notes/1", `{"title":"A2","body":"y"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, want 200", rec.Code)
	}
	var n Note
	if err := json.Unmarshal(rec.Body.Bytes(), &n); err != nil {
		t.Fatalf("update body: %v", err)
	}
	if n.ID != 1 || n.Title != "A2" || n.Body != "y" {
		t.Errorf("updated = %+v", n)
	}
	if rec := do(t, h, http.MethodPut, "/notes/99", `{"title":"X","body":""}`); rec.Code != http.StatusNotFound {
		t.Errorf("update missing = %d, want 404", rec.Code)
	}
	if rec := do(t, h, http.MethodPut, "/notes/1", `{"title":"","body":"z"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("update empty title = %d, want 400", rec.Code)
	}
}

func TestDelete(t *testing.T) {
	h := New()
	do(t, h, http.MethodPost, "/notes", `{"title":"A","body":""}`)
	if rec := do(t, h, http.MethodDelete, "/notes/1", ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete = %d, want 204", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/notes/1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", rec.Code)
	}
}

func TestBadIDAndMethod(t *testing.T) {
	h := New()
	if rec := do(t, h, http.MethodGet, "/notes/abc", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("non-numeric id = %d, want 400", rec.Code)
	}
	if rec := do(t, h, http.MethodDelete, "/notes", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /notes = %d, want 405", rec.Code)
	}
}

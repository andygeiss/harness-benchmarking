package tasks

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

func TestCreateDefaultsDoneFalse(t *testing.T) {
	h := New()
	rec := do(t, h, http.MethodPost, "/tasks", `{"title":"write tests"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", rec.Code)
	}
	var task Task
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatalf("create body: %v", err)
	}
	if task.ID != 1 || task.Title != "write tests" || task.Done {
		t.Fatalf("created = %+v, want {1 write tests false}", task)
	}

	rec = do(t, h, http.MethodGet, "/tasks/1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, want 200", rec.Code)
	}
	var got Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("get body: %v", err)
	}
	if got != task {
		t.Errorf("get = %+v, want %+v", got, task)
	}
}

func TestCreateRequiresTitle(t *testing.T) {
	h := New()
	if rec := do(t, h, http.MethodPost, "/tasks", `{"title":""}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty title = %d, want 400", rec.Code)
	}
}

func TestListInsertionOrder(t *testing.T) {
	h := New()
	do(t, h, http.MethodPost, "/tasks", `{"title":"A"}`)
	do(t, h, http.MethodPost, "/tasks", `{"title":"B"}`)
	rec := do(t, h, http.MethodGet, "/tasks", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200", rec.Code)
	}
	var ts []Task
	if err := json.Unmarshal(rec.Body.Bytes(), &ts); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(ts) != 2 || ts[0].Title != "A" || ts[1].Title != "B" {
		t.Errorf("list = %+v, want [A, B] in order", ts)
	}
}

func TestUpdateSetsDone(t *testing.T) {
	h := New()
	do(t, h, http.MethodPost, "/tasks", `{"title":"A"}`)

	rec := do(t, h, http.MethodPut, "/tasks/1", `{"title":"A done","done":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, want 200", rec.Code)
	}
	var task Task
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatalf("update body: %v", err)
	}
	if task.ID != 1 || task.Title != "A done" || !task.Done {
		t.Errorf("updated = %+v, want {1 A done true}", task)
	}

	if rec := do(t, h, http.MethodPut, "/tasks/99", `{"title":"X","done":true}`); rec.Code != http.StatusNotFound {
		t.Errorf("update missing = %d, want 404", rec.Code)
	}
	if rec := do(t, h, http.MethodPut, "/tasks/1", `{"title":"","done":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("update empty title = %d, want 400", rec.Code)
	}
}

func TestDelete(t *testing.T) {
	h := New()
	do(t, h, http.MethodPost, "/tasks", `{"title":"A"}`)
	if rec := do(t, h, http.MethodDelete, "/tasks/1", ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete = %d, want 204", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/tasks/1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", rec.Code)
	}
	if rec := do(t, h, http.MethodDelete, "/tasks/1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("delete missing = %d, want 404", rec.Code)
	}
}

func TestBadIDAndMethod(t *testing.T) {
	h := New()
	if rec := do(t, h, http.MethodGet, "/tasks/abc", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("non-numeric id = %d, want 400", rec.Code)
	}
	if rec := do(t, h, http.MethodDelete, "/tasks", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /tasks = %d, want 405", rec.Code)
	}
}

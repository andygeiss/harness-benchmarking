package users

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
	rec := do(t, h, http.MethodPost, "/users", `{"name":"Ann","email":"ann@x.io"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", rec.Code)
	}
	var u User
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil {
		t.Fatalf("create body: %v", err)
	}
	if u.ID != 1 || u.Name != "Ann" || u.Email != "ann@x.io" {
		t.Fatalf("created = %+v, want {1 Ann ann@x.io}", u)
	}

	rec = do(t, h, http.MethodGet, "/users/1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, want 200", rec.Code)
	}
	var got User
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("get body: %v", err)
	}
	if got != u {
		t.Errorf("get = %+v, want %+v", got, u)
	}
}

func TestCreateValidation(t *testing.T) {
	h := New()
	for _, body := range []string{
		`{"name":"","email":"a@b.c"}`,
		`{"name":"A","email":""}`,
		`{"name":"A","email":"no-at-sign"}`,
	} {
		if rec := do(t, h, http.MethodPost, "/users", body); rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s = %d, want 400", body, rec.Code)
		}
	}
}

func TestCreateDuplicateEmail(t *testing.T) {
	h := New()
	do(t, h, http.MethodPost, "/users", `{"name":"Ann","email":"dup@x.io"}`)
	rec := do(t, h, http.MethodPost, "/users", `{"name":"Bob","email":"dup@x.io"}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate email = %d, want 409", rec.Code)
	}
}

func TestListInsertionOrder(t *testing.T) {
	h := New()
	do(t, h, http.MethodPost, "/users", `{"name":"A","email":"a@x.io"}`)
	do(t, h, http.MethodPost, "/users", `{"name":"B","email":"b@x.io"}`)

	rec := do(t, h, http.MethodGet, "/users", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200", rec.Code)
	}
	var us []User
	if err := json.Unmarshal(rec.Body.Bytes(), &us); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(us) != 2 || us[0].Name != "A" || us[1].Name != "B" {
		t.Errorf("list = %+v, want [A, B] in order", us)
	}
}

func TestGetMissingAndBadID(t *testing.T) {
	h := New()
	if rec := do(t, h, http.MethodGet, "/users/99", ""); rec.Code != http.StatusNotFound {
		t.Errorf("missing get = %d, want 404", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/users/abc", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("non-numeric id = %d, want 400", rec.Code)
	}
}

func TestUpdate(t *testing.T) {
	h := New()
	do(t, h, http.MethodPost, "/users", `{"name":"Ann","email":"ann@x.io"}`)

	rec := do(t, h, http.MethodPut, "/users/1", `{"name":"Ann2","email":"ann2@x.io"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, want 200", rec.Code)
	}
	var u User
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil {
		t.Fatalf("update body: %v", err)
	}
	if u.ID != 1 || u.Name != "Ann2" || u.Email != "ann2@x.io" {
		t.Errorf("updated = %+v", u)
	}

	if rec := do(t, h, http.MethodPut, "/users/99", `{"name":"X","email":"x@y.z"}`); rec.Code != http.StatusNotFound {
		t.Errorf("update missing = %d, want 404", rec.Code)
	}
	if rec := do(t, h, http.MethodPut, "/users/1", `{"name":"","email":"x@y.z"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("update invalid = %d, want 400", rec.Code)
	}
}

func TestUpdateDuplicateEmail(t *testing.T) {
	h := New()
	do(t, h, http.MethodPost, "/users", `{"name":"A","email":"a@x.io"}`)
	do(t, h, http.MethodPost, "/users", `{"name":"B","email":"b@x.io"}`)
	rec := do(t, h, http.MethodPut, "/users/2", `{"name":"B","email":"a@x.io"}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("update to duplicate email = %d, want 409", rec.Code)
	}
}

func TestDelete(t *testing.T) {
	h := New()
	do(t, h, http.MethodPost, "/users", `{"name":"Ann","email":"ann@x.io"}`)

	if rec := do(t, h, http.MethodDelete, "/users/1", ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete = %d, want 204", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/users/1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", rec.Code)
	}
	if rec := do(t, h, http.MethodDelete, "/users/1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("delete missing = %d, want 404", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := New()
	if rec := do(t, h, http.MethodDelete, "/users", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /users = %d, want 405", rec.Code)
	}
}

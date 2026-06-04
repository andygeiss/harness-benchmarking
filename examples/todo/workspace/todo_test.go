package main

// todo_test.go is the fixed specification for the htmx todo app. There is no
// browser in `go test`, so htmx's client-side behaviour is verified indirectly:
// every handler returned by NewServer must produce the HTML — status codes, bare
// fragments, hx-* wiring, escaping — that htmx needs to drive the UI.
//
// The agent implements NewServer and the templates it renders; it may not edit
// this file. Assertions check rendered behaviour, not exact markup, so the
// implementation is free as long as the contract holds.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// do issues one request against h and returns the recorded response. A non-nil
// form is sent url-encoded as the body.
func do(t *testing.T, h http.Handler, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// addTodo posts a new todo and fails the test if creation does not return 200.
func addTodo(t *testing.T, h http.Handler, text string) *httptest.ResponseRecorder {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/todos", url.Values{"text": {text}})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /todos %q: status = %d, want 200\nbody: %s", text, rec.Code, rec.Body.String())
	}
	return rec
}

func mustContain(t *testing.T, where, body, sub string) {
	t.Helper()
	if !strings.Contains(body, sub) {
		t.Errorf("%s: response missing %q\n--- body ---\n%s", where, sub, body)
	}
}

func mustNotContain(t *testing.T, where, body, sub string) {
	t.Helper()
	if strings.Contains(body, sub) {
		t.Errorf("%s: response unexpectedly contains %q\n--- body ---\n%s", where, sub, body)
	}
}

func TestIndexRendersShell(t *testing.T) {
	rec := do(t, NewServer(), http.MethodGet, "/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("GET /: Content-Type = %q, want to contain text/html", ct)
	}
	body := rec.Body.String()
	mustContain(t, "GET /", body, "/static/htmx.min.js")  // htmx is loaded
	mustContain(t, "GET /", body, "/static/app.css")      // styles are loaded
	mustContain(t, "GET /", body, `id="todos"`)           // the container htmx appends into
	mustContain(t, "GET /", body, `hx-post="/todos"`)     // the add form posts via htmx
	mustContain(t, "GET /", body, `name="text"`)          // ...carrying the new todo text
	mustNotContain(t, "GET / (empty)", body, `id="todo-`) // no items on a fresh store
}

func TestAddReturnsItemFragment(t *testing.T) {
	rec := addTodo(t, NewServer(), "Buy milk")
	body := rec.Body.String()
	mustContain(t, "POST /todos", body, "Buy milk")
	mustContain(t, "POST /todos", body, `id="todo-1"`)               // first todo gets id 1
	mustContain(t, "POST /todos", body, `hx-post="/todos/1/toggle"`) // toggle wired to this id
	mustContain(t, "POST /todos", body, `hx-delete="/todos/1"`)      // delete wired to this id
	mustNotContain(t, "POST /todos", body, "checked")                // a new todo is not done
	// The fragment is a bare <li>, not a whole page — htmx swaps it into the list.
	lower := strings.ToLower(body)
	mustNotContain(t, "POST /todos fragment", lower, "<html")
	mustNotContain(t, "POST /todos fragment", lower, "<!doctype")
}

func TestAddRejectsBlank(t *testing.T) {
	srv := NewServer()
	for _, text := range []string{"", "   ", "\t\n"} {
		rec := do(t, srv, http.MethodPost, "/todos", url.Values{"text": {text}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST /todos text=%q: status = %d, want 400", text, rec.Code)
		}
	}
	body := do(t, srv, http.MethodGet, "/", nil).Body.String()
	mustNotContain(t, "GET / after blank adds", body, `id="todo-`) // nothing was created
}

func TestAddEscapesHTML(t *testing.T) {
	rec := addTodo(t, NewServer(), `<script>alert('xss')</script>`)
	body := rec.Body.String()
	mustContain(t, "XSS add", body, "&lt;script&gt;") // text is HTML-escaped
	mustNotContain(t, "XSS add", body, "<script>")    // ...never emitted raw
}

func TestToggleFlipsDone(t *testing.T) {
	srv := NewServer()
	addTodo(t, srv, "Walk the dog")

	rec := do(t, srv, http.MethodPost, "/todos/1/toggle", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle: status = %d, want 200", rec.Code)
	}
	on := rec.Body.String()
	mustContain(t, "toggle on", on, `id="todo-1"`)
	mustContain(t, "toggle on", on, "checked") // now done

	off := do(t, srv, http.MethodPost, "/todos/1/toggle", nil).Body.String()
	mustNotContain(t, "toggle off", off, "checked") // toggled back to not done
}

func TestToggleUnknownIsNotFound(t *testing.T) {
	srv := NewServer()
	for _, target := range []string{"/todos/999/toggle", "/todos/abc/toggle"} {
		rec := do(t, srv, http.MethodPost, target, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: status = %d, want 404", target, rec.Code)
		}
	}
}

func TestDeleteRemovesItem(t *testing.T) {
	srv := NewServer()
	addTodo(t, srv, "DeleteMe")
	addTodo(t, srv, "KeepMe")

	rec := do(t, srv, http.MethodDelete, "/todos/1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /todos/1: status = %d, want 200", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "" {
		t.Errorf("DELETE /todos/1: body = %q, want empty (htmx swaps the item away)", got)
	}

	body := do(t, srv, http.MethodGet, "/", nil).Body.String()
	mustNotContain(t, "GET / after delete", body, "DeleteMe")
	mustNotContain(t, "GET / after delete", body, `id="todo-1"`)
	mustContain(t, "GET / after delete", body, "KeepMe")
	mustContain(t, "GET / after delete", body, `id="todo-2"`)
}

func TestDeleteUnknownIsNotFound(t *testing.T) {
	rec := do(t, NewServer(), http.MethodDelete, "/todos/999", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("DELETE /todos/999: status = %d, want 404", rec.Code)
	}
}

func TestIDsIncrementAndOrderIsStable(t *testing.T) {
	srv := NewServer()
	mustContain(t, "add alpha", addTodo(t, srv, "alpha").Body.String(), `id="todo-1"`)
	mustContain(t, "add beta", addTodo(t, srv, "beta").Body.String(), `id="todo-2"`)
	mustContain(t, "add gamma", addTodo(t, srv, "gamma").Body.String(), `id="todo-3"`)

	body := do(t, srv, http.MethodGet, "/", nil).Body.String()
	a, b, g := strings.Index(body, "alpha"), strings.Index(body, "beta"), strings.Index(body, "gamma")
	if a < 0 || b < 0 || g < 0 {
		t.Fatalf("GET /: missing an item: alpha=%d beta=%d gamma=%d", a, b, g)
	}
	if !(a < b && b < g) {
		t.Errorf("GET /: items out of insertion order: alpha=%d beta=%d gamma=%d", a, b, g)
	}
}

func TestUnsupportedMethodIsRejected(t *testing.T) {
	// /todos is registered for POST only; a GET to it must not fall through to
	// the page — the stdlib ServeMux answers 405 for a known path, wrong method.
	rec := do(t, NewServer(), http.MethodGet, "/todos", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /todos: status = %d, want 405", rec.Code)
	}
}

func TestStaticAssetsServed(t *testing.T) {
	srv := NewServer()

	css := do(t, srv, http.MethodGet, "/static/app.css", nil)
	if css.Code != http.StatusOK {
		t.Fatalf("GET /static/app.css: status = %d, want 200", css.Code)
	}
	if ct := css.Header().Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Errorf("app.css: Content-Type = %q, want to contain text/css", ct)
	}

	js := do(t, srv, http.MethodGet, "/static/htmx.min.js", nil)
	if js.Code != http.StatusOK {
		t.Fatalf("GET /static/htmx.min.js: status = %d, want 200", js.Code)
	}
	if ct := js.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("htmx.min.js: Content-Type = %q, want to contain javascript", ct)
	}
	mustContain(t, "htmx.min.js", js.Body.String(), "htmx") // the real library, from embed.FS
}

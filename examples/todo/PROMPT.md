# Task: a server-rendered htmx todo app

Build a small web app — package `main` — that serves a todo list and mutates it
through **htmx** partial updates. The build currently fails because the package
has no implementation; the provided test (`todo_test.go`) drives the HTTP
handlers and is your specification.

This is a Go-idiomatic web task: `net/http` routing, `html/template` rendering,
and `embed.FS` for templates and static assets. It is larger than the other
examples and is meant to take several passes — decompose it and track progress
in PROGRESS.md.

## Entry point

    func NewServer() http.Handler

`NewServer` returns an `http.Handler` backed by a **fresh in-memory store** (each
call starts empty). Tests construct a new server per test, so all state lives in
that handler. Also provide `func main()` that serves it (e.g. on `:8080`), so the
app can be run with `go run .` and opened in a browser.

## Routes and behaviour

- `GET /` — the full HTML page: a `<head>` that loads `/static/app.css` and
  `/static/htmx.min.js`, an add form, and `<ul id="todos">` listing the current
  todos in insertion order. Respond with `Content-Type: text/html`.
- `POST /todos` — form field `text`. Trim it; if it is empty, respond **400** and
  add nothing. Otherwise create a todo (ids start at 1 and increase) and respond
  **200** with the **bare `<li>` fragment** for the new item (no `<html>`
  wrapper) — htmx appends it to `#todos`.
- `POST /todos/{id}/toggle` — flip the item's done flag; respond **200** with the
  updated `<li>` fragment. Unknown or non-numeric id → **404**.
- `DELETE /todos/{id}` — remove the item; respond **200** with an empty body
  (htmx swaps the element away). Unknown or non-numeric id → **404**.
- `GET /static/...` — serve the embedded `static/` directory (the provided
  `htmx.min.js` and `app.css`).
- A known path requested with an unsupported method must return **405**. The
  standard library's `http.ServeMux` (Go 1.22+ method patterns) does this for
  you, so route `GET /`, `POST /todos`, … with method+path patterns and use
  `r.PathValue("id")`.

## The `<li>` item

Each todo renders as `<li id="todo-{id}" class="todo">` (add ` done` to the class
when completed — the provided CSS styles `.todo` and `.todo.done`) containing:

- a checkbox wired to toggle — `hx-post="/todos/{id}/toggle"`,
  `hx-target="#todo-{id}"`, `hx-swap="outerHTML"`, and the `checked` attribute
  when the todo is done;
- the todo text, rendered through `html/template` so user input is **HTML-escaped**;
- a delete control wired with `hx-delete="/todos/{id}"`.

The full page and the fragment responses must render the *same* item markup —
define it once (a named template) and reuse it for both.

## Provided (do not modify)

- `todo_test.go` — the spec.
- `static/htmx.min.js`, `static/app.css` — embed and serve these as they are.
  You do **not** need to read them — `htmx.min.js` is a large minified library;
  just embed the `static/` directory and serve it at `/static/`.

## You create

- `main.go` (you may split across files) — the store, the handlers, the `embed`
  directives, and template parsing.
- `templates/*.html` — the page layout and the item fragment. `embed` needs these
  files to exist for the package to compile.

## Rules

- Use only the Go standard library (`net/http`, `html/template`, `embed`, …).
- Keep the in-memory store safe for concurrent use.
- Do not modify `todo_test.go` or the files under `static/`.
- You are done when `go test ./...` passes.

## How to approach it (several passes)

Your context resets between passes; read PROGRESS.md first and keep it current. A
natural decomposition: (1) the store + `GET /` page; (2) `POST /todos` returning
the item fragment, sharing one item template; (3) toggle; (4) delete; (5) the
static assets and the 404/400/405 edge cases. Run `go test ./...` after each
stage to see how far you have gotten.

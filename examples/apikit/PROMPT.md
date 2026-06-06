# Task: a small modular JSON HTTP API

Implement the module `apikit`: a JSON-over-HTTP service built from several
**independent feature packages**, plus one package that composes them. The build
currently fails because every package has tests but no implementation; the tests
(`*_test.go` in each package) are your specification.

This is a **large, multi-package task** meant to take **many passes**: implement
one package, run its tests, record progress in `PROGRESS.md`, and continue. The
feature packages do not depend on each other — only `api` imports them — so you
can build them one at a time, in any order, and verify each in isolation.

## Conventions every package follows

These hold across all feature packages; the tests encode them precisely.

- **Constructor.** Each package exposes `func New() http.Handler` that returns a
  handler backed by a **fresh in-memory store** (no global state; a second
  `New()` shares nothing with the first). Route with the Go 1.22 method+pattern
  mux (`"POST /users"`, `"GET /users/{id}"`, …) so unsupported methods on a known
  path return **405** for free.
- **JSON.** Responses are `application/json`. A resource is encoded with an
  integer `id` (assigned by the server, starting at **1** per store) plus its
  fields. Errors are `{"error":"<message>"}` with the status below.
- **IDs and ordering.** `id` increments by one per created resource. `GET`
  collection endpoints return a JSON array in **insertion order** (equivalently,
  ascending `id`).
- **Status codes.** create → **201**; get/list/update → **200**; delete →
  **204** (empty body); validation failure → **400**; unknown `id` → **404**; a
  non-numeric `id` in the path → **400**; a uniqueness violation → **409**;
  unsupported method on a known path → **405**.

## Packages and contracts

### `apikit/health` — liveness (smallest; a good warm-up)

`GET /healthz` → `200` with body `{"status":"ok"}`. Any other method on
`/healthz` → `405`.

### `apikit/users` — CRUD over users

Resource: `{"id":int,"name":string,"email":string}`.

- `POST /users` `{"name","email"}` → `201` the created user. `name` or `email`
  empty → `400`; `email` without an `@` → `400`; an `email` already in use → `409`.
- `GET /users` → `200` array in insertion order.
- `GET /users/{id}` → `200` the user, or `404`.
- `PUT /users/{id}` `{"name","email"}` → `200` the updated user, or `404`;
  same validation as create; changing to an `email` used by **another** user → `409`.
- `DELETE /users/{id}` → `204`, or `404`.

### `apikit/tasks` — CRUD over tasks

Resource: `{"id":int,"title":string,"done":bool}`.

- `POST /tasks` `{"title"}` → `201`; `done` starts `false`; empty `title` → `400`.
- `GET /tasks` → `200` array in insertion order.
- `GET /tasks/{id}` → `200` or `404`.
- `PUT /tasks/{id}` `{"title","done"}` → `200` or `404`; empty `title` → `400`.
- `DELETE /tasks/{id}` → `204` or `404`.

### `apikit/notes` — CRUD over notes

Resource: `{"id":int,"title":string,"body":string}`.

- `POST /notes` `{"title","body"}` → `201`; empty `title` → `400` (`body` optional).
- `GET /notes`, `GET /notes/{id}`, `PUT /notes/{id}`, `DELETE /notes/{id}` —
  same shapes and codes as the others.

### `apikit/api` — composition (implement last; imports the others)

`func New() http.Handler` mounts the feature handlers on one mux so the whole
service is reachable from a single handler:

- `/healthz` → the `health` handler.
- `/users` and `/users/{id}` → the `users` handler (delegate the whole subtree;
  do not strip the prefix — the feature handler matches the absolute paths).
- `/tasks`, `/tasks/{id}` → `tasks`; `/notes`, `/notes/{id}` → `notes`.
- Any other path → `404` with body `{"error":"not found"}`.

## Rules

- Put each package in its own directory under the module root, matching the test
  files' package and import paths (`apikit/users`, `apikit/api`, …).
- Do not modify any `*_test.go` file — they are the fixed specification.
- Use only the Go standard library.
- You are done when `go test ./...` passes.

## How to approach it (many passes)

Your context resets between passes; read `PROGRESS.md` first and keep it current.
Build the independent features first, each a checkpoint to verify before moving on,
then compose them:

1. `health` — one endpoint; confirms the JSON/405 shape end to end.
2. `users` — the full CRUD pattern, including `409` on duplicate email.
3. `tasks` — CRUD with a boolean field.
4. `notes` — CRUD with an optional field.
5. `api` — mount the four handlers and add the catch-all `404`.

Run `go test ./<pkg>/...` after each package; `go test ./...` is the final gate.

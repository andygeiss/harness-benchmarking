# PROGRESS.md

## Status
- [x] health — Liveness endpoint (`GET /healthz` → `{"status":"ok"}`)
- [x] users — CRUD for users with email validation and uniqueness
- [x] tasks — CRUD for tasks with boolean done field
- [x] notes — CRUD for notes with optional body field
- [x] api — Composition of all feature handlers with catch-all 404

## Final Result
`go test ./...` passes — all 5 packages implemented and verified.

# Task: implement stringutil.Reverse

The `stringutil` package has a test file (`stringutil/reverse_test.go`) but no
implementation, so the build fails. Create `stringutil/reverse.go` so that
`go test ./...` passes.

## Spec

Implement:

    func Reverse(s string) string

Return `s` with its characters reversed. Reverse by **rune**, not by byte, so
multi-byte UTF-8 characters (accented letters, emoji) stay intact. The empty
string reverses to the empty string.

## Rules

- Put the function in package `stringutil`, in the file `stringutil/reverse.go`.
- Do not modify `reverse_test.go`.
- Use only the Go standard library.
- You are done when `go test ./...` passes.

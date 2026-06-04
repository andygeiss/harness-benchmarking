# Task: implement the textkit string helpers

Implement package `textkit` so that the provided tests (`textkit/textkit_test.go`)
pass. The build currently fails because the package has no implementation.

## Contract

Implement these independent functions, each in package `textkit`:

    func WordCount(s string) int               // number of whitespace-separated words
    func CountVowels(s string) int             // count of a, e, i, o, u (case-insensitive, ASCII)
    func IsPalindrome(s string) bool           // exact reads-the-same-backwards, by rune; "" is a palindrome
    func Acronym(s string) string              // first letter of each word, upper-cased: "portable network graphics" -> "PNG"
    func RunLengthEncode(s string) string      // "aaabbc" -> "3a2b1c"; "" -> ""
    func RunLengthDecode(s string) (string, error) // inverse of RunLengthEncode; error on malformed input
    func Rotate(s string, n int) string        // rotate runes left by n (n may exceed len or be negative); "abcde",2 -> "cdeab"
    func Title(s string) string                // upper-case the first letter of each word and lower-case the rest, words joined by single spaces

Each function is small, self-contained, and specified entirely by its test
cases — read the test to see the exact expected values, including the edge cases
(empty input, the error cases for `RunLengthDecode`, negative/large `n` for
`Rotate`, whitespace handling for `Title`).

## How to approach it (this will take several passes)

Your context is reset between passes, so implement the functions a few at a time
and track which are done in PROGRESS.md. The functions are independent, so a
clean plan is simply a checklist:

- [ ] WordCount
- [ ] CountVowels
- [ ] IsPalindrome
- [ ] Acronym
- [ ] RunLengthEncode
- [ ] RunLengthDecode
- [ ] Rotate
- [ ] Title

After implementing some, run `go test ./...` to see how far you have gotten,
update the checklist in PROGRESS.md, and continue. Read PROGRESS.md first on
every pass — it is your memory across resets, so you do not have to re-derive
what is already done.

## Rules

- Put the implementation in package `textkit` (you may split it across files).
- Do not modify `textkit_test.go`.
- Use only the Go standard library.
- You are done when `go test ./...` passes.

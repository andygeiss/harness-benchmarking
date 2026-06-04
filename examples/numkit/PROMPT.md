# Task: implement the numkit numeric helpers

Implement package `numkit` so that the provided tests (`numkit/numkit_test.go`)
pass. The build currently fails because the package has no implementation.

## Contract

Implement these independent functions, each in package `numkit`:

    func GCD(a, b int) int        // greatest common divisor; GCD(0, 0) == 0
    func IsPrime(n int) bool      // n < 2 is not prime
    func DigitSum(n int) int      // sum of the decimal digits of abs(n)
    func Factorial(n int) int     // n! for n >= 0; Factorial(0) == 1
    func Fib(n int) int           // Fibonacci; Fib(0) == 0, Fib(1) == 1

Each function is small, self-contained, and specified entirely by its test
cases — read the test to see the exact expected values.

## How to approach it (this may take more than one pass)

Your context is reset between passes, so implement the functions a few at a time
and track which are done in PROGRESS.md. The functions are independent, so a
clean plan is simply a checklist:

- [ ] GCD
- [ ] IsPrime
- [ ] DigitSum
- [ ] Factorial
- [ ] Fib

After implementing some, run `go test ./...` to see how far you have gotten,
update the checklist in PROGRESS.md, and continue. Read PROGRESS.md first on
every pass — it is your memory across resets.

## Rules

- Put the implementation in package `numkit` (you may split it across files).
- Do not modify `numkit_test.go`.
- Use only the Go standard library.
- You are done when `go test ./...` passes.

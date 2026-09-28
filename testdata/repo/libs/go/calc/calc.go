// Package calc is a fixture for coverreport's golden test.
package calc

import "errors"

// ErrNegative is returned for negative input.
var ErrNegative = errors.New("negative")

// Classify labels n.
func Classify(n int) (string, error) {
	if n < 0 {
		return "", ErrNegative
	}

	// Small numbers get a name.
	switch {
	case n == 0:
		return "zero", nil
	case n < 10:
		return "small", nil
	}
	return "large", nil
}

// Sum adds a slice.
func Sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

// Unused is never called.
func Unused() int {
	a := 1
	b := 2

	return a + b
}

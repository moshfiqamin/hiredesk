package domain

import (
	"errors"
	"strconv"
	"strings"
)

// ErrInvalidMoney means a string is not an amount ParseMoney accepts.
var ErrInvalidMoney = errors.New("not a valid amount")

// maxMoneyWholeDigits is how many digits may precede the decimal point. It
// mirrors the numeric(12, 2) columns: twelve digits in total, two of them
// after the point.
const maxMoneyWholeDigits = 10

// Money is an amount in minor units: 8500050 is 85000.50.
//
// An integer, never a float64: 0.1 has no exact binary representation, so
// float arithmetic on money drifts, and two amounts that should be equal
// compare unequal.
type Money int64

// ParseMoney reads a plain decimal amount such as "85000", "85000.5" or
// "85000.50". It returns ErrInvalidMoney for anything else: a sign, an
// exponent, more than two decimal places, or more than ten digits before the
// point.
func ParseMoney(s string) (Money, error) {
	whole, frac, hasPoint := strings.Cut(s, ".")

	if whole == "" || len(whole) > maxMoneyWholeDigits || !isDigits(whole) {
		return 0, ErrInvalidMoney
	}
	// "85000." has a point and nothing after it.
	if hasPoint && (frac == "" || len(frac) > 2 || !isDigits(frac)) {
		return 0, ErrInvalidMoney
	}

	// Pad to exactly two places, so "85000.5" is 50 minor units, not 5.
	frac += strings.Repeat("0", 2-len(frac))

	// At most twelve digits, which always fits in an int64. The error is
	// still checked rather than assumed away.
	minor, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return 0, ErrInvalidMoney
	}
	return Money(minor), nil
}

// String formats m with exactly two decimal places, as ParseMoney reads it:
// "85000.50".
func (m Money) String() string {
	sign := ""
	if m < 0 {
		sign = "-"
		m = -m
	}

	whole := strconv.FormatInt(int64(m/100), 10)
	frac := strconv.FormatInt(int64(m%100), 10)
	if len(frac) == 1 {
		frac = "0" + frac
	}
	return sign + whole + "." + frac
}

// isDigits reports whether s consists only of the ASCII digits 0 to 9. It is
// true for the empty string; callers check for that separately.
func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

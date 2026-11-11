package domain

import (
	"errors"
	"testing"
)

func TestParseMoney(t *testing.T) {
	t.Parallel()

	valid := []struct {
		in   string
		want Money
	}{
		{"0", 0},
		{"85000", 8500000},
		{"85000.5", 8500050},
		{"85000.50", 8500050},
		{"85000.05", 8500005},
		{"0.01", 1},
		{"007", 700},
		{"9999999999.99", 999999999999}, // the largest value numeric(12, 2) holds
	}
	for _, tt := range valid {
		got, err := ParseMoney(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseMoney(%q) = %d, %v; want %d, nil", tt.in, got, err, tt.want)
		}
	}

	invalid := []string{
		"", ".", ".5", "85000.", "85000.505", "-1", "+1", "1e3", "1,000", " 1", "1 ",
		"abc", "1.2.3", "٣", // an Arabic-Indic digit: a digit to unicode.IsDigit, not to PostgreSQL
		"10000000000", // eleven digits before the point overflows the column
	}
	for _, in := range invalid {
		if got, err := ParseMoney(in); !errors.Is(err, ErrInvalidMoney) {
			t.Errorf("ParseMoney(%q) = %d, %v; want ErrInvalidMoney", in, got, err)
		}
	}
}

func TestMoneyString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   Money
		want string
	}{
		{0, "0.00"},
		{1, "0.01"},
		{50, "0.50"},
		{8500000, "85000.00"},
		{8500005, "85000.05"},
		{-150, "-1.50"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("Money(%d).String() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Whatever String produces, ParseMoney must read back as the same amount.
func TestMoneyRoundTrip(t *testing.T) {
	t.Parallel()

	for _, m := range []Money{0, 1, 99, 100, 101, 8500050, 999999999999} {
		got, err := ParseMoney(m.String())
		if err != nil || got != m {
			t.Errorf("ParseMoney(%q) = %d, %v; want %d", m.String(), got, err, m)
		}
	}
}

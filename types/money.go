// Package types holds the request, response and enum types exchanged with the
// Groww Trading API. It has no dependencies outside the standard library and
// performs no I/O.
package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// paisePerRupee is the fixed scale of Money: two decimal places.
const paisePerRupee = 100

// ErrMoneyPrecision is returned when a value carries more precision than paise
// can represent, e.g. "10.005". We refuse rather than round, because silently
// rounding a price is how an SDK loses someone money.
var ErrMoneyPrecision = errors.New("types: amount has finer precision than paise")

// Money is an exact monetary amount in Indian rupees, stored as a signed count
// of paise (1/100 rupee).
//
// Money is deliberately not a float64 and cannot be built from one. Decimal
// strings are parsed digit by digit, so no binary rounding is ever introduced:
// 0.1 + 0.2 is exactly 0.3 here, which is not true of float64.
//
// The zero value is ₹0.00 and is ready to use. Money is a comparable value
// type — use == or Cmp, and pass it by value.
type Money struct {
	paise int64
}

// FromPaise builds a Money from a raw paise count.
func FromPaise(p int64) Money { return Money{paise: p} }

// FromRupees builds a Money from a whole number of rupees.
// It panics if the result would overflow int64.
func FromRupees(r int64) Money { return Money{paise: mulOverflow(r, paisePerRupee)} }

// Paise returns the amount as a count of paise.
func (m Money) Paise() int64 { return m.paise }

// ParseMoney parses a decimal string such as "1234.50", "-0.05" or "42" into
// Money. It accepts at most two decimal places; additional digits are allowed
// only if they are zero, otherwise it returns ErrMoneyPrecision.
//
// Exponent notation ("1e6") is rejected: the API does not emit it, and
// accepting it would mean routing through a float.
func ParseMoney(s string) (Money, error) {
	raw := s
	s = strings.TrimSpace(s)
	if s == "" {
		return Money{}, fmt.Errorf("types: parse money %q: empty string", raw)
	}

	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		neg = true
		s = s[1:]
	}

	intPart, fracPart, hasDot := strings.Cut(s, ".")
	if intPart == "" && fracPart == "" {
		return Money{}, fmt.Errorf("types: parse money %q: no digits", raw)
	}
	if hasDot && fracPart == "" {
		return Money{}, fmt.Errorf("types: parse money %q: trailing decimal point", raw)
	}
	if intPart == "" {
		intPart = "0"
	}
	if err := checkDigits(intPart); err != nil {
		return Money{}, fmt.Errorf("types: parse money %q: %w", raw, err)
	}
	if err := checkDigits(fracPart); err != nil {
		return Money{}, fmt.Errorf("types: parse money %q: %w", raw, err)
	}

	// Normalise the fraction to exactly two digits.
	switch {
	case len(fracPart) < 2:
		fracPart += strings.Repeat("0", 2-len(fracPart))
	case len(fracPart) > 2:
		if strings.Trim(fracPart[2:], "0") != "" {
			return Money{}, fmt.Errorf("types: parse money %q: %w", raw, ErrMoneyPrecision)
		}
		fracPart = fracPart[:2]
	}

	digits := intPart + fracPart
	if neg {
		digits = "-" + digits
	}
	// ParseInt does the overflow check for us; there is no path here that
	// silently wraps.
	p, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("types: parse money %q: out of range", raw)
	}
	return Money{paise: p}, nil
}

func checkDigits(s string) error {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return fmt.Errorf("invalid character %q", s[i])
		}
	}
	return nil
}

// String renders the amount with exactly two decimal places, e.g. "-1234.50".
func (m Money) String() string {
	neg := m.paise < 0
	// Negating math.MinInt64 overflows, so take the magnitude in uint64.
	var u uint64
	if neg {
		u = uint64(-(m.paise + 1)) + 1
	} else {
		u = uint64(m.paise)
	}

	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteString(strconv.FormatUint(u/paisePerRupee, 10))
	b.WriteByte('.')
	if frac := u % paisePerRupee; frac < 10 {
		b.WriteByte('0')
		b.WriteString(strconv.FormatUint(frac, 10))
	} else {
		b.WriteString(strconv.FormatUint(frac, 10))
	}
	return b.String()
}

// MarshalJSON emits an unquoted JSON number, which is what the API expects.
func (m Money) MarshalJSON() ([]byte, error) {
	return []byte(m.String()), nil
}

// UnmarshalJSON accepts a JSON number (1234.5), a quoted decimal ("1234.50")
// or null. The bytes are parsed as a decimal string, never via float64.
func (m *Money) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		return nil
	}
	if len(s) >= 2 && s[0] == '"' {
		var quoted string
		if err := json.Unmarshal(b, &quoted); err != nil {
			return err
		}
		s = quoted
	}
	v, err := ParseMoney(s)
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// Add returns m+o. It panics on int64 overflow, which for real amounts is
// unreachable (int64 paise spans roughly ±₹92 trillion) and therefore signals
// corrupt input rather than a recoverable condition.
func (m Money) Add(o Money) Money {
	sum := m.paise + o.paise
	// Overflow can only happen when both operands share a sign and the result
	// does not.
	if (m.paise > 0 && o.paise > 0 && sum < 0) || (m.paise < 0 && o.paise < 0 && sum >= 0) {
		panic("types: Money.Add overflow")
	}
	return Money{paise: sum}
}

// Sub returns m-o. It panics on int64 overflow.
func (m Money) Sub(o Money) Money {
	diff := m.paise - o.paise
	if (o.paise < 0 && diff < m.paise) || (o.paise > 0 && diff > m.paise) {
		panic("types: Money.Sub overflow")
	}
	return Money{paise: diff}
}

// MulInt returns m*n, for cases like unit price × quantity.
// It panics on int64 overflow.
func (m Money) MulInt(n int64) Money {
	return Money{paise: mulOverflow(m.paise, n)}
}

// Neg returns -m. It panics on int64 overflow.
func (m Money) Neg() Money {
	if m.paise == math.MinInt64 {
		panic("types: Money.Neg overflow")
	}
	return Money{paise: -m.paise}
}

// Abs returns |m|. It panics on int64 overflow.
func (m Money) Abs() Money {
	if m.paise < 0 {
		return m.Neg()
	}
	return m
}

// Cmp reports whether m is less than (-1), equal to (0) or greater than (+1) o.
func (m Money) Cmp(o Money) int {
	switch {
	case m.paise < o.paise:
		return -1
	case m.paise > o.paise:
		return 1
	default:
		return 0
	}
}

// Sign returns -1, 0 or +1.
func (m Money) Sign() int { return m.Cmp(Money{}) }

// IsZero reports whether the amount is exactly ₹0.00.
func (m Money) IsZero() bool { return m.paise == 0 }

func mulOverflow(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	p := a * b
	if p/b != a || (a == math.MinInt64 && b == -1) {
		panic("types: Money multiplication overflow")
	}
	return p
}

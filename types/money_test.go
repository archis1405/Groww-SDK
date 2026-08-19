package types

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestParseMoney(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantPaise int64
		wantErr   error // nil, ErrMoneyPrecision, or errAny for "some error"
	}{
		{name: "whole rupees", in: "1234", wantPaise: 123400},
		{name: "two decimals", in: "1234.50", wantPaise: 123450},
		{name: "one decimal", in: "1234.5", wantPaise: 123450},
		{name: "leading dot", in: ".05", wantPaise: 5},
		{name: "zero", in: "0", wantPaise: 0},
		{name: "zero with decimals", in: "0.00", wantPaise: 0},
		{name: "negative", in: "-0.05", wantPaise: -5},
		{name: "negative whole", in: "-1234.50", wantPaise: -123450},
		{name: "explicit plus", in: "+7.25", wantPaise: 725},
		{name: "surrounding space", in: "  10.10  ", wantPaise: 1010},
		{name: "tick size", in: "0.05", wantPaise: 5},
		{name: "trailing zeros are not precision", in: "10.5000", wantPaise: 1050},
		{name: "leading zeros", in: "007.50", wantPaise: 750},

		{name: "too much precision", in: "10.005", wantErr: ErrMoneyPrecision},
		{name: "third digit nonzero", in: "1.239", wantErr: ErrMoneyPrecision},
		{name: "empty", in: "", wantErr: errAny},
		{name: "sign only", in: "-", wantErr: errAny},
		{name: "trailing dot", in: "12.", wantErr: errAny},
		{name: "exponent rejected", in: "1e6", wantErr: errAny},
		{name: "letters", in: "12a.00", wantErr: errAny},
		{name: "two dots", in: "1.2.3", wantErr: errAny},
		{name: "overflow", in: "99999999999999999999", wantErr: errAny},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMoney(tt.in)
			switch {
			case tt.wantErr == nil:
				if err != nil {
					t.Fatalf("ParseMoney(%q) unexpected error: %v", tt.in, err)
				}
				if got.Paise() != tt.wantPaise {
					t.Errorf("ParseMoney(%q) = %d paise, want %d", tt.in, got.Paise(), tt.wantPaise)
				}
			case errors.Is(tt.wantErr, errAny):
				if err == nil {
					t.Fatalf("ParseMoney(%q) = %v, want an error", tt.in, got)
				}
			default:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ParseMoney(%q) error = %v, want %v", tt.in, err, tt.wantErr)
				}
			}
		})
	}
}

// errAny is a sentinel meaning "any error will do" in the table above.
var errAny = errors.New("any error")

func TestMoneyString(t *testing.T) {
	tests := []struct {
		name  string
		paise int64
		want  string
	}{
		{name: "zero value", paise: 0, want: "0.00"},
		{name: "sub rupee", paise: 5, want: "0.05"},
		{name: "ten paise", paise: 10, want: "0.10"},
		{name: "whole", paise: 123400, want: "1234.00"},
		{name: "mixed", paise: 123450, want: "1234.50"},
		{name: "negative sub rupee", paise: -5, want: "-0.05"},
		{name: "negative mixed", paise: -123450, want: "-1234.50"},
		{name: "max int64", paise: math.MaxInt64, want: "92233720368547758.07"},
		{name: "min int64 does not overflow", paise: math.MinInt64, want: "-92233720368547758.08"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FromPaise(tt.paise).String(); got != tt.want {
				t.Errorf("FromPaise(%d).String() = %q, want %q", tt.paise, got, tt.want)
			}
		})
	}
}

func TestMoneyRoundTripJSON(t *testing.T) {
	tests := []struct {
		name      string
		json      string
		wantPaise int64
		wantOut   string
	}{
		{name: "bare number", json: `1234.5`, wantPaise: 123450, wantOut: "1234.50"},
		{name: "quoted", json: `"1234.50"`, wantPaise: 123450, wantOut: "1234.50"},
		{name: "integer", json: `1234`, wantPaise: 123400, wantOut: "1234.00"},
		{name: "negative", json: `-0.05`, wantPaise: -5, wantOut: "-0.05"},
		{name: "zero", json: `0`, wantPaise: 0, wantOut: "0.00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m Money
			if err := json.Unmarshal([]byte(tt.json), &m); err != nil {
				t.Fatalf("Unmarshal(%s): %v", tt.json, err)
			}
			if m.Paise() != tt.wantPaise {
				t.Fatalf("Unmarshal(%s) = %d paise, want %d", tt.json, m.Paise(), tt.wantPaise)
			}
			out, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(out) != tt.wantOut {
				t.Errorf("Marshal = %s, want %s", out, tt.wantOut)
			}
		})
	}
}

func TestMoneyUnmarshalNullLeavesValue(t *testing.T) {
	m := FromPaise(999)
	if err := json.Unmarshal([]byte("null"), &m); err != nil {
		t.Fatalf("Unmarshal(null): %v", err)
	}
	if m.Paise() != 999 {
		t.Errorf("null overwrote value: got %d paise, want 999", m.Paise())
	}
}

func TestMoneyUnmarshalInStruct(t *testing.T) {
	// The field is a value, not a pointer — this checks that the pointer
	// receiver on UnmarshalJSON is still reached by encoding/json.
	var got struct {
		Price Money `json:"price"`
		Qty   int64 `json:"qty"`
	}
	if err := json.Unmarshal([]byte(`{"price":2450.75,"qty":3}`), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Price.Paise() != 245075 {
		t.Fatalf("price = %d paise, want 245075", got.Price.Paise())
	}
	if want := "7352.25"; got.Price.MulInt(got.Qty).String() != want {
		t.Errorf("price*qty = %s, want %s", got.Price.MulInt(got.Qty), want)
	}
}

func TestMoneyArithmetic(t *testing.T) {
	tests := []struct {
		name string
		got  Money
		want string
	}{
		{name: "add", got: mustParse(t, "0.10").Add(mustParse(t, "0.20")), want: "0.30"},
		{name: "add negative", got: mustParse(t, "10.00").Add(mustParse(t, "-2.50")), want: "7.50"},
		{name: "sub", got: mustParse(t, "10.00").Sub(mustParse(t, "10.05")), want: "-0.05"},
		{name: "mul", got: mustParse(t, "2450.75").MulInt(4), want: "9803.00"},
		{name: "mul by zero", got: mustParse(t, "2450.75").MulInt(0), want: "0.00"},
		{name: "mul negative", got: mustParse(t, "2450.75").MulInt(-2), want: "-4901.50"},
		{name: "neg", got: mustParse(t, "-1.25").Neg(), want: "1.25"},
		{name: "abs", got: mustParse(t, "-1.25").Abs(), want: "1.25"},
		{name: "abs of positive", got: mustParse(t, "1.25").Abs(), want: "1.25"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.got.String(); got != tt.want {
				t.Errorf("= %s, want %s", got, tt.want)
			}
		})
	}
}

// The whole reason Money exists: this identity fails for float64.
func TestMoneyNoFloatRounding(t *testing.T) {
	sum := Money{}
	tenPaise := FromPaise(10)
	for range 10 {
		sum = sum.Add(tenPaise)
	}
	if want := "1.00"; sum.String() != want {
		t.Errorf("ten × 0.10 = %s, want %s", sum, want)
	}
}

func TestMoneyOverflowPanics(t *testing.T) {
	tests := []struct {
		name string
		fn   func()
	}{
		{name: "add", fn: func() { FromPaise(math.MaxInt64).Add(FromPaise(1)) }},
		{name: "add negative", fn: func() { FromPaise(math.MinInt64).Add(FromPaise(-1)) }},
		{name: "sub", fn: func() { FromPaise(math.MinInt64).Sub(FromPaise(1)) }},
		{name: "mul", fn: func() { FromPaise(math.MaxInt64).MulInt(2) }},
		{name: "neg min", fn: func() { FromPaise(math.MinInt64).Neg() }},
		{name: "from rupees", fn: func() { FromRupees(math.MaxInt64) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected a panic on overflow, got none")
				}
			}()
			tt.fn()
		})
	}
}

func TestMoneyCmpAndSign(t *testing.T) {
	tests := []struct {
		name     string
		a, b     Money
		wantCmp  int
		wantSign int
	}{
		{name: "less", a: FromPaise(-1), b: FromPaise(0), wantCmp: -1, wantSign: -1},
		{name: "equal", a: FromPaise(500), b: FromPaise(500), wantCmp: 0, wantSign: 1},
		{name: "greater", a: FromPaise(501), b: FromPaise(500), wantCmp: 1, wantSign: 1},
		{name: "zero", a: Money{}, b: Money{}, wantCmp: 0, wantSign: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Cmp(tt.b); got != tt.wantCmp {
				t.Errorf("Cmp = %d, want %d", got, tt.wantCmp)
			}
			if got := tt.a.Sign(); got != tt.wantSign {
				t.Errorf("Sign = %d, want %d", got, tt.wantSign)
			}
			if got := tt.a.IsZero(); got != (tt.a.Paise() == 0) {
				t.Errorf("IsZero = %v", got)
			}
		})
	}
}

func mustParse(t *testing.T, s string) Money {
	t.Helper()
	m, err := ParseMoney(s)
	if err != nil {
		t.Fatalf("ParseMoney(%q): %v", s, err)
	}
	return m
}

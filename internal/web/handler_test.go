package web

import "testing"

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0, "0.00"},
		{1.5, "1.50"},
		{999.99, "999.99"},
		{1000, "1,000.00"},
		{12340.00, "12,340.00"},
		{1234567.89, "1,234,567.89"},
		{0.01, "0.01"},
		{100, "100.00"},
	}

	for _, tt := range tests {
		got := formatNumber(tt.input)
		if got != tt.want {
			t.Errorf("formatNumber(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatGBP(t *testing.T) {
	f := funcMap["formatGBP"].(func(float64) string)

	tests := []struct {
		input float64
		want  string
	}{
		{4521.30, "£4,521.30"},
		{0, "£0.00"},
		{-100.50, "-£100.50"},
		{12340, "£12,340.00"},
	}

	for _, tt := range tests {
		got := f(tt.input)
		if got != tt.want {
			t.Errorf("formatGBP(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

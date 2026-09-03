package setup

import (
	"encoding/json"
	"testing"
)

func TestJSONPositiveInt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   any
		want int
	}{
		{nil, 0},
		{0, 0},
		{-1, 0},
		{8192, 8192},
		{int64(4096), 4096},
		{float64(16384), 16384},
		{json.Number("32768"), 32768},
		{"8192", 0},
	}
	for _, tc := range cases {
		if got := jsonPositiveInt(tc.in); got != tc.want {
			t.Fatalf("jsonPositiveInt(%v %T) = %d, want %d", tc.in, tc.in, got, tc.want)
		}
	}
}

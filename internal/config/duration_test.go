package config

import (
	"testing"
	"time"
)

func TestDurationUnmarshalText(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"5m", 5 * time.Minute, true},
		{"2h", 2 * time.Hour, true},
		{"1h30m", 90 * time.Minute, true},
		{"500ms", 500 * time.Millisecond, true},
		{"nonsense", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		var d Duration
		err := d.UnmarshalText([]byte(c.in))
		if c.ok {
			if err != nil {
				t.Errorf("%q: unexpected error %v", c.in, err)
				continue
			}
			if d.Duration != c.want {
				t.Errorf("%q = %v, want %v", c.in, d.Duration, c.want)
			}
		} else if err == nil {
			t.Errorf("%q: expected error, got none", c.in)
		}
	}
}

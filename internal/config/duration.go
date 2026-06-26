package config

import "time"

// Duration is a time.Duration that unmarshals from a TOML string such as "5m"
// or "2h". go-toml/v2 has no native duration type, so a plain time.Duration
// field would not bind from a config file; this wrapper implements
// encoding.TextUnmarshaler so go-toml decodes the string form.
type Duration struct{ time.Duration }

// UnmarshalText parses a Go duration string (time.ParseDuration).
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

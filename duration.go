package sbox

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration wrapper that supports YAML unmarshaling
// from human-readable duration strings (e.g. "5s", "1m30s", "0").
type Duration struct {
	time.Duration
}

func (d Duration) MarshalYAML() (any, error) {
	return d.Duration.String(), nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string: %w", err)
	}

	// "0" is a special case: zero duration (infinite wait in startup-delay context)
	if s == "0" {
		d.Duration = 0
		return nil
	}

	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}

	d.Duration = parsed
	return nil
}

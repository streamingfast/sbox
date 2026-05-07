package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsFlag(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// Real flags
		{"-v", true},
		{"--verbose", true},
		{"--docker-socket", true},
		{"-w", true},
		{"--profile", true},

		// Prompts that start with dashes but are NOT flags
		{"---", false},
		{"----", false},
		{"---\ntitle: foo", false},

		// Plain prompts
		{"implement the feature", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, isFlag(tt.input))
		})
	}
}

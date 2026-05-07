package sbox

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPrintLoopReport_Confirmed_SingleIteration(t *testing.T) {
	var buf strings.Builder
	ui := NewUI(&buf)

	ui.PrintLoopReport(LoopReport{
		Outcome:       "confirmed",
		Iterations:    1,
		Completions:   2,
		Required:      2,
		TotalDuration: 35 * time.Second,
		PerIteration:  []time.Duration{35 * time.Second},
	})

	out := buf.String()
	assert.Contains(t, out, "Loop Summary")
	assert.Contains(t, out, "Status")
	assert.Contains(t, out, "Goal confirmed")
	assert.Contains(t, out, "Total")
	assert.Contains(t, out, "35s")
	// Single iteration: no "Iteration 1" row
	assert.NotContains(t, out, "Iteration 1")
}

func TestPrintLoopReport_Confirmed_MultiIteration(t *testing.T) {
	var buf strings.Builder
	ui := NewUI(&buf)

	ui.PrintLoopReport(LoopReport{
		Outcome:       "confirmed",
		Iterations:    2,
		Completions:   2,
		Required:      2,
		TotalDuration: 35 * time.Second,
		PerIteration:  []time.Duration{17 * time.Second, 18 * time.Second},
	})

	out := buf.String()
	assert.Contains(t, out, "Loop Summary")
	assert.Contains(t, out, "Goal confirmed")
	assert.Contains(t, out, "Total")
	// Multi-iteration: both iteration rows shown
	assert.Contains(t, out, "Iteration 1")
	assert.Contains(t, out, "Iteration 2")
	assert.Contains(t, out, "17s")
	assert.Contains(t, out, "18s")
}

func TestPrintLoopReport_MaxReached(t *testing.T) {
	var buf strings.Builder
	ui := NewUI(&buf)

	ui.PrintLoopReport(LoopReport{
		Outcome:       "max_reached",
		Iterations:    1,
		MaxIterations: 1,
		TotalDuration: 35 * time.Second,
		PerIteration:  []time.Duration{35 * time.Second},
	})

	out := buf.String()
	assert.Contains(t, out, "Loop Summary")
	assert.Contains(t, out, "Max iterations reached")
	assert.Contains(t, out, "Total")
	assert.Contains(t, out, "35s")
	// Single iteration: no "Iteration 1" row
	assert.NotContains(t, out, "Iteration 1")
}

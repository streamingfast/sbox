package sbox

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"
)

// UI styles for consistent terminal output across sbox commands
var (
	StyleHeader  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))  // blue
	StyleSuccess = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))  // green
	StyleWarn    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))  // yellow
	StyleError   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))  // red
	StyleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))             // dim gray
	StyleLabel   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("7"))  // white bold
)

// UI provides styled terminal output for sbox commands.
type UI struct {
	w io.Writer
}

// DefaultUI is the package-level UI used by backends for status messages.
var DefaultUI = NewUI(os.Stdout)

// NewUI creates a UI that writes to w.
func NewUI(w io.Writer) *UI {
	return &UI{w: w}
}

// Status prints a dim informational status line (sandbox lifecycle, etc.)
func (u *UI) Status(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(u.w, StyleDim.Render(msg))
}

// Label prints a key-value pair with a bold label.
// The value is collapsed to a single line and truncated to 100 characters if needed.
func (u *UI) Label(key, value string) {
	fmt.Fprintf(u.w, "%s %s\n", StyleLabel.Render(key+":"), truncateLabel(value, 100))
}

// truncateLabel collapses newlines to spaces and truncates s to max runes,
// appending "..." if the string was longer (keeping total length at max).
func truncateLabel(s string, max int) string {
	// Collapse newlines/carriage returns to a single space
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")

	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}

// Header prints a prominent section header.
func (u *UI) Header(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(u.w, StyleHeader.Render(msg))
}

// Success prints a green success message.
func (u *UI) Success(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(u.w, StyleSuccess.Render("✓ "+msg))
}

// Warn prints a yellow warning message.
func (u *UI) Warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(u.w, StyleWarn.Render("⚠ "+msg))
}

// Error prints a red error message.
func (u *UI) Error(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(u.w, StyleError.Render("✗ "+msg))
}

// Blank prints an empty line.
func (u *UI) Blank() {
	fmt.Fprintln(u.w)
}

// Loop-specific helpers

// Iteration prints the loop iteration header.
func (u *UI) Iteration(n, completions int) {
	fmt.Fprintln(u.w)
	fmt.Fprintln(u.w, StyleHeader.Render(fmt.Sprintf("── Iteration %d ──", n)))
}

// Completed prints the completion confirmation message.
func (u *UI) Completed(count, required int) {
	fmt.Fprintln(u.w, StyleSuccess.Render(fmt.Sprintf("✓ Goal completed (%d/%d)", count, required)))
}

// Confirmed prints the final success message with timing summary.
func (u *UI) Confirmed(iterations int, total time.Duration, perIteration []time.Duration) {
	fmt.Fprintln(u.w)
	fmt.Fprintln(u.w, StyleSuccess.Render(fmt.Sprintf("✓ Goal confirmed complete after %d iterations", iterations)))
	u.printTimingSummary(total, perIteration)
}

// Continuing prints the "still working" status.
func (u *UI) Continuing() {
	fmt.Fprintln(u.w, StyleDim.Render("Goal not yet complete, continuing..."))
}

// Reconfirming prints the "re-running to confirm" status.
func (u *UI) Reconfirming() {
	fmt.Fprintln(u.w, StyleDim.Render("Re-running to confirm..."))
}

// MaxReached prints the max iterations exceeded message with timing summary.
func (u *UI) MaxReached(max int, total time.Duration, perIteration []time.Duration) {
	fmt.Fprintln(u.w, StyleWarn.Render(fmt.Sprintf("⚠ Reached maximum iterations (%d)", max)))
	u.printTimingSummary(total, perIteration)
}

// printTimingSummary prints the global and per-iteration timing report.
func (u *UI) printTimingSummary(total time.Duration, perIteration []time.Duration) {
	fmt.Fprintln(u.w)
	fmt.Fprintln(u.w, StyleHeader.Render("── Timing Summary ──"))
	fmt.Fprintf(u.w, "%s %s\n", StyleLabel.Render("Total:"), formatDuration(total))
	for i, d := range perIteration {
		fmt.Fprintf(u.w, "%s %s\n", StyleLabel.Render(fmt.Sprintf("Iteration %d:", i+1)), formatDuration(d))
	}
}

// formatDuration formats a duration in a human-friendly way,
// rounding to the nearest second for durations >= 1 minute.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	if seconds == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

// AgentError prints an agent error message.
func (u *UI) AgentError(err error) {
	fmt.Fprintln(u.w, StyleError.Render(fmt.Sprintf("✗ Agent error: %s", err)))
}

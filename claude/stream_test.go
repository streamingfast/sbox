package claude

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintMarkdown_NoExtraBlankLines(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantLineCount int
		wantContains  []string
	}{
		{
			name: "simple paragraph",
			input: `Hello world`,
			wantLineCount: 1,
			wantContains:  []string{"Hello world"},
		},
		{
			name: "heading and body",
			input: `## The Bug

There's an error.`,
			wantLineCount: 2,
			wantContains:  []string{"The Bug", "There's an error."},
		},
		{
			name: "markdown with multiple sections no extra blank lines",
			input: `Fix a React hooks bug.

## The Bug

There's a "Rendered more hooks than during the previous render" error.

## Task List

- Read the file fully
- Identify the conditional hook call
- Fix the conditional hook call`,
			// heading + body + heading + 3 list items = 7 visual lines (no blank lines)
			wantLineCount: 7,
			wantContains:  []string{"The Bug", "Task List", "Read the file fully"},
		},
		{
			name: "blockquote should not produce blank lines",
			input: `> This is a blockquote
> with multiple lines`,
			// Should produce content lines only, no blank lines between blockquote lines
			wantContains: []string{"This is a blockquote"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			p := NewStreamPrinter(&buf)
			p.printMarkdown(tt.input)

			output := buf.String()
			require.NotEmpty(t, output, "printMarkdown should produce output for non-empty input")

			// Count non-empty visual lines in output
			lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
			var nonBlankLines []string
			for _, line := range lines {
				if strings.TrimSpace(ansi.Strip(line)) != "" {
					nonBlankLines = append(nonBlankLines, line)
				}
			}

			// Verify no consecutive blank lines (no extra blank lines)
			prevBlank := false
			for _, line := range lines {
				isBlank := strings.TrimSpace(ansi.Strip(line)) == ""
				if isBlank && prevBlank {
					t.Errorf("found consecutive blank lines in output:\n%s", output)
					break
				}
				prevBlank = isBlank
			}

			if tt.wantLineCount > 0 {
				assert.Equal(t, tt.wantLineCount, len(nonBlankLines),
					"unexpected number of non-blank lines in output:\n%s", output)
			}

			strippedOutput := ansi.Strip(output)
			for _, want := range tt.wantContains {
				assert.Contains(t, strippedOutput, want,
					"output should contain %q but got:\n%s", want, strippedOutput)
			}
		})
	}
}

func TestPrintMarkdown_FirstLineHasBulletPrefix(t *testing.T) {
	var buf bytes.Buffer
	p := NewStreamPrinter(&buf)
	p.printMarkdown("Hello world")

	output := buf.String()
	strippedOutput := ansi.Strip(output)

	lines := strings.Split(strings.TrimRight(strippedOutput, "\n"), "\n")
	require.NotEmpty(t, lines)
	assert.Contains(t, lines[0], "● ", "first line should have the ● prefix")
	assert.Contains(t, lines[0], "Hello world")
}

func TestPrintMarkdown_SubsequentLinesHaveIndent(t *testing.T) {
	var buf bytes.Buffer
	p := NewStreamPrinter(&buf)
	p.printMarkdown(`## Heading

Body line`)

	output := buf.String()
	strippedOutput := ansi.Strip(output)

	lines := strings.Split(strings.TrimRight(strippedOutput, "\n"), "\n")
	var nonBlank []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonBlank = append(nonBlank, l)
		}
	}

	require.Len(t, nonBlank, 2, "should have heading and body lines")
	assert.Contains(t, nonBlank[0], "● ", "first line should have ● prefix")
	assert.True(t, strings.HasPrefix(nonBlank[1], "  "), "subsequent lines should have 2-space indent")
}

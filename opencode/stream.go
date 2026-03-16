package opencode

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	glamour "charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	lipgloss "charm.land/lipgloss/v2"
)

// Stream event types from OpenCode --format=json
const (
	EventTypeStepStart  = "step_start"
	EventTypeStepFinish = "step_finish"
	EventTypeToolUse    = "tool_use"
	EventTypeText       = "text"
)

// streamEvent is the top-level envelope for OpenCode JSON stream events.
type streamEvent struct {
	Type      string    `json:"type"`
	Timestamp int64     `json:"timestamp"`
	SessionID string    `json:"sessionID"`
	Part      eventPart `json:"part"`
}

type eventPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`

	// For text events
	Text string `json:"text,omitempty"`

	// For tool_use events
	CallID string    `json:"callID,omitempty"`
	Tool   string    `json:"tool,omitempty"`
	State  toolState `json:"state,omitempty"`

	// For step_finish events
	Reason string     `json:"reason,omitempty"`
	Cost   float64    `json:"cost,omitempty"`
	Tokens tokenUsage `json:"tokens,omitempty"`

	// For step_start events
	Snapshot string `json:"snapshot,omitempty"`
}

type toolState struct {
	Status   string       `json:"status"`
	Input    toolInput    `json:"input,omitempty"`
	Output   string       `json:"output,omitempty"`
	Title    string       `json:"title,omitempty"`
	Metadata toolMetadata `json:"metadata,omitempty"`
	Time     toolTime     `json:"time,omitempty"`
}

type toolInput struct {
	// bash tool
	Command     string `json:"command,omitempty"`
	Description string `json:"description,omitempty"`

	// file tools
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`

	// edit tool
	FilePath string `json:"file_path,omitempty"`
	OldStr   string `json:"old_str,omitempty"`
	NewStr   string `json:"new_str,omitempty"`
}

type toolMetadata struct {
	Output      string `json:"output,omitempty"`
	Exit        int    `json:"exit"`
	Description string `json:"description,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
}

type toolTime struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type tokenUsage struct {
	Total     int        `json:"total"`
	Input     int        `json:"input"`
	Output    int        `json:"output"`
	Reasoning int        `json:"reasoning"`
	Cache     tokenCache `json:"cache"`
}

type tokenCache struct {
	Read  int `json:"read"`
	Write int `json:"write"`
}

// Styles — shared with claude package for visual consistency
var (
	dotStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))  // blue (tool calls)
	dotOkStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))  // green (tool success)
	dotErrStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))  // red (tool error)
	dotTextStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("7"))  // white (text output)
	toolStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("7"))  // white bold
	argStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))             // dim
	textStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))             // white
	resultStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))  // green
	errorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))  // red
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))             // gray
	unknownStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))  // yellow
)

const (
	dot       = "● "
	resultPfx = "  ⎿  "
)

// StreamPrinter processes OpenCode JSON stream lines and prints human-readable output.
type StreamPrinter struct {
	w         io.Writer
	md        *glamour.TermRenderer
	lastPrint string // tracks what was last printed: "tool", "result", "text", "step"

	// Accumulated cost/token stats across steps
	totalTokens int
	totalCost   float64
	steps       int
}

// newStreamStyle returns a glamour style customized for sbox stream output.
// Identical to the Claude stream style for visual consistency.
func newStreamStyle() ansi.StyleConfig {
	purple := "105"
	zero := uint(0)

	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: stringPtr("252"),
			},
			Margin: &zero,
		},
		Paragraph: ansi.StyleBlock{},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: stringPtr("39"),
				Bold:  boolPtr(true),
			},
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: &purple,
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: stringPtr("244"),
				},
				Indent: uintPtr(2),
				Margin: &zero,
			},
			Chroma: &ansi.Chroma{
				Text:             ansi.StylePrimitive{Color: stringPtr("#C4C4C4")},
				Comment:          ansi.StylePrimitive{Color: stringPtr("#676767")},
				Keyword:          ansi.StylePrimitive{Color: stringPtr("#00AAFF")},
				KeywordReserved:  ansi.StylePrimitive{Color: stringPtr("#FF5FD2")},
				KeywordNamespace: ansi.StylePrimitive{Color: stringPtr("#FF5F87")},
				KeywordType:      ansi.StylePrimitive{Color: stringPtr("#6E6ED8")},
				Operator:         ansi.StylePrimitive{Color: stringPtr("#EF8080")},
				NameFunction:     ansi.StylePrimitive{Color: stringPtr("#00D787")},
				NameBuiltin:      ansi.StylePrimitive{Color: stringPtr("#FF8EC7")},
				LiteralString:    ansi.StylePrimitive{Color: stringPtr("#C69669")},
				LiteralNumber:    ansi.StylePrimitive{Color: stringPtr("#6EEFC0")},
				GenericDeleted:   ansi.StylePrimitive{Color: stringPtr("#FD5B5B")},
				GenericInserted:  ansi.StylePrimitive{Color: stringPtr("#00D787")},
			},
		},
		Strong: ansi.StylePrimitive{Bold: boolPtr(true)},
		Emph:   ansi.StylePrimitive{Italic: boolPtr(true)},
		Link: ansi.StylePrimitive{
			Color:     stringPtr("30"),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: stringPtr("35"),
			Bold:  boolPtr(true),
		},
		List: ansi.StyleList{
			LevelIndent: 2,
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
	}
}

func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }
func uintPtr(u uint) *uint       { return &u }

// NewStreamPrinter creates a new StreamPrinter that writes to w.
func NewStreamPrinter(w io.Writer) *StreamPrinter {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithStyles(newStreamStyle()),
		glamour.WithWordWrap(100),
	)
	return &StreamPrinter{w: w, md: renderer}
}

// ProcessLine parses a single OpenCode JSON stream line and prints formatted output.
func (p *StreamPrinter) ProcessLine(line string) bool {
	if len(line) == 0 {
		return false
	}

	var event streamEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return false
	}

	switch event.Type {
	case EventTypeToolUse:
		return p.handleToolUse(&event)
	case EventTypeText:
		return p.handleText(&event)
	case EventTypeStepFinish:
		return p.handleStepFinish(&event)
	case EventTypeStepStart:
		// Step starts are not displayed
		return false
	default:
		fmt.Fprintf(p.w, "%s %s\n", unknownStyle.Render("? Unknown event type:"), dimStyle.Render(event.Type))
		return true
	}
}

func (p *StreamPrinter) handleToolUse(event *streamEvent) bool {
	part := &event.Part
	toolName := displayName(part.Tool)
	arg := toolArg(part)

	// Blank line before each tool call for readability
	if p.lastPrint != "" {
		fmt.Fprintln(p.w)
	}

	// Print tool invocation
	if arg != "" {
		fmt.Fprintf(p.w, "%s%s(%s)\n", dotStyle.Render(dot), toolStyle.Render(toolName), argStyle.Render(arg))
	} else {
		fmt.Fprintf(p.w, "%s%s\n", dotStyle.Render(dot), toolStyle.Render(toolName))
	}

	// Print tool result if completed
	if part.State.Status == "completed" {
		p.printToolResult(part)
	} else if part.State.Status == "error" {
		errOutput := truncate(strings.TrimSpace(part.State.Output), 200)
		if errOutput == "" {
			errOutput = "Tool error"
		}
		fmt.Fprintf(p.w, "%s%s\n", dotErrStyle.Render(resultPfx), errorStyle.Render(errOutput))
	}

	p.lastPrint = "tool"
	return true
}

func (p *StreamPrinter) printToolResult(part *eventPart) {
	output := strings.TrimSpace(part.State.Output)
	if output == "" {
		output = strings.TrimSpace(part.State.Metadata.Output)
	}

	if part.State.Metadata.Exit != 0 {
		errOut := truncate(output, 200)
		if errOut == "" {
			errOut = fmt.Sprintf("exit code %d", part.State.Metadata.Exit)
		}
		fmt.Fprintf(p.w, "%s%s\n", dotErrStyle.Render(resultPfx), errorStyle.Render(errOut))
		return
	}

	if output != "" {
		fmt.Fprintf(p.w, "%s%s\n", dotOkStyle.Render(resultPfx), dimStyle.Render(truncate(output, 200)))
	} else {
		fmt.Fprintf(p.w, "%s%s\n", dotOkStyle.Render(resultPfx), dimStyle.Render("(No output)"))
	}
}

func (p *StreamPrinter) handleText(event *streamEvent) bool {
	text := strings.TrimSpace(event.Part.Text)
	if text == "" {
		return false
	}

	if p.lastPrint != "" {
		fmt.Fprintln(p.w)
	}

	p.printMarkdown(text)
	p.lastPrint = "text"
	return true
}

func (p *StreamPrinter) handleStepFinish(event *streamEvent) bool {
	part := &event.Part
	p.steps++
	p.totalTokens += part.Tokens.Total
	p.totalCost += part.Cost

	// Only print summary on final step (reason="stop" means the agent is done)
	if part.Reason == "stop" {
		if p.lastPrint == "tool" {
			fmt.Fprintln(p.w)
		}
		fmt.Fprintf(p.w, "%s %s\n", resultStyle.Render("✓ Done"),
			dimStyle.Render(fmt.Sprintf("(%d steps, %d tokens)", p.steps, p.totalTokens)))
		p.lastPrint = "result"
		return true
	}

	return false
}

// printMarkdown renders text as markdown using glamour, with a ● prefix on the first line.
func (p *StreamPrinter) printMarkdown(text string) {
	if p.md == nil {
		fmt.Fprintf(p.w, "%s%s\n", dotTextStyle.Render(dot), textStyle.Render(text))
		return
	}

	rendered, err := p.md.Render(text)
	if err != nil {
		fmt.Fprintf(p.w, "%s%s\n", dotTextStyle.Render(dot), textStyle.Render(text))
		return
	}

	rendered = strings.TrimSpace(rendered)
	if rendered == "" {
		return
	}

	lines := strings.Split(rendered, "\n")
	first := true
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if first {
			fmt.Fprintf(p.w, "%s%s\n", dotTextStyle.Render(dot), line)
			first = false
		} else {
			fmt.Fprintf(p.w, "  %s\n", line)
		}
	}
}

// toolArg extracts the primary argument for display from a tool_use event.
func toolArg(part *eventPart) string {
	switch part.Tool {
	case "bash":
		if desc := part.State.Input.Description; desc != "" {
			return truncate(desc, 80)
		}
		return truncate(part.State.Input.Command, 80)
	case "read", "write":
		return shortenPath(part.State.Input.Path)
	case "edit":
		path := part.State.Input.FilePath
		if path == "" {
			path = part.State.Input.Path
		}
		return shortenPath(path)
	}

	// Use title as fallback
	if part.State.Title != "" {
		return truncate(part.State.Title, 80)
	}
	return ""
}

// displayName maps OpenCode tool names to display names.
func displayName(name string) string {
	switch name {
	case "bash":
		return "Bash"
	case "read":
		return "Read"
	case "write":
		return "Write"
	case "edit":
		return "Update"
	case "glob":
		return "Glob"
	case "grep":
		return "Grep"
	default:
		return name
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// shortenPath returns a shorter display path by using only the last 2 components.
func shortenPath(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return path
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

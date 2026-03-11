package claude

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	glamour "charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	lipgloss "charm.land/lipgloss/v2"
)

// Stream event types from Claude Code --output-format=stream-json
const (
	EventTypeSystem    = "system"
	EventTypeAssistant = "assistant"
	EventTypeUser      = "user"
	EventTypeResult    = "result"
	EventTypeRateLimit = "rate_limit_event"
)

// Content block types within assistant messages
const (
	ContentTypeThinking = "thinking"
	ContentTypeToolUse  = "tool_use"
	ContentTypeText     = "text"
)

// maxDiffLines is the maximum number of diff lines to display for Edit results.
const maxDiffLines = 20

// streamEvent is the top-level envelope for all stream-json events.
type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// For assistant/user messages
	Message *streamMessage `json:"message,omitempty"`

	// For tool_result events (type=user)
	ToolUseResult *toolUseResult `json:"tool_use_result,omitempty"`

	// For result events
	Result       string  `json:"result,omitempty"`
	StopReason   string  `json:"stop_reason,omitempty"`
	DurationMs   int     `json:"duration_ms,omitempty"`
	NumTurns     int     `json:"num_turns,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	IsError      bool    `json:"is_error,omitempty"`
}

type streamMessage struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`

	// For text blocks
	Text string `json:"text,omitempty"`

	// For thinking blocks
	Thinking string `json:"thinking,omitempty"`

	// For tool_use blocks
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// For tool_result blocks (in user messages)
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type toolUseResult struct {
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
	Type   string `json:"type,omitempty"`

	// For file reads
	File *fileResult `json:"file,omitempty"`

	// For edits
	StructuredPatch []patchEntry `json:"structuredPatch,omitempty"`
}

type fileResult struct {
	FilePath   string `json:"filePath,omitempty"`
	NumLines   int    `json:"numLines,omitempty"`
	TotalLines int    `json:"totalLines,omitempty"`
}

type patchEntry struct {
	OldStart int         `json:"oldStart"`
	OldLines int         `json:"oldLines"`
	NewStart int         `json:"newStart"`
	NewLines int         `json:"newLines"`
	Lines    []patchLine `json:"lines"`
}

type patchLine struct {
	Type    string `json:"type"` // "context", "add", "remove"
	Content string `json:"content"`
	OldNum  int    `json:"oldNum,omitempty"`
	NewNum  int    `json:"newNum,omitempty"`
}

// Styles for pretty-printing
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
	thinkStyle   = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("5")) // magenta
	addStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))             // green
	removeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))             // red
	lineNumStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))             // dim
	unknownStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3")) // yellow
)

const (
	dot       = "● "
	result    = "  ⎿  "
	userArrow = "❯ "
)

// StreamPrinter processes Claude stream-json lines and prints human-readable output.
type StreamPrinter struct {
	w         io.Writer
	md        *glamour.TermRenderer
	lastPrint string // tracks what was last printed: "tool", "result", "text", "thinking"
	lastTool  string // tracks the last tool name for result formatting
}

// newStreamStyle returns a glamour style customized for sbox stream output.
// Based on the dark style with purple inline code and no document margins
// (we handle indentation ourselves with the ● prefix).
func newStreamStyle() ansi.StyleConfig {
	purple := "105"    // blue-purple for inline code
	zero := uint(0)

	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: stringPtr("252"),
			},
			Margin: &zero, // no margin — we handle indentation
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
				Text:              ansi.StylePrimitive{Color: stringPtr("#C4C4C4")},
				Comment:           ansi.StylePrimitive{Color: stringPtr("#676767")},
				Keyword:           ansi.StylePrimitive{Color: stringPtr("#00AAFF")},
				KeywordReserved:   ansi.StylePrimitive{Color: stringPtr("#FF5FD2")},
				KeywordNamespace:  ansi.StylePrimitive{Color: stringPtr("#FF5F87")},
				KeywordType:       ansi.StylePrimitive{Color: stringPtr("#6E6ED8")},
				Operator:          ansi.StylePrimitive{Color: stringPtr("#EF8080")},
				NameFunction:      ansi.StylePrimitive{Color: stringPtr("#00D787")},
				NameBuiltin:       ansi.StylePrimitive{Color: stringPtr("#FF8EC7")},
				LiteralString:     ansi.StylePrimitive{Color: stringPtr("#C69669")},
				LiteralNumber:     ansi.StylePrimitive{Color: stringPtr("#6EEFC0")},
				GenericDeleted:    ansi.StylePrimitive{Color: stringPtr("#FD5B5B")},
				GenericInserted:   ansi.StylePrimitive{Color: stringPtr("#00D787")},
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

// ProcessLine parses a single stream-json line and prints formatted output.
// Returns true if the line was handled, false if it was skipped/unknown.
func (p *StreamPrinter) ProcessLine(line string) bool {
	if len(line) == 0 {
		return false
	}

	var event streamEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return false
	}

	switch event.Type {
	case EventTypeAssistant:
		return p.handleAssistant(&event)
	case EventTypeUser:
		return p.handleUser(&event)
	case EventTypeResult:
		return p.handleResult(&event)
	case EventTypeSystem, EventTypeRateLimit:
		return false
	default:
		fmt.Fprintf(p.w, "%s %s\n", unknownStyle.Render("? Unknown event type:"), dimStyle.Render(event.Type))
		return true
	}
}

func (p *StreamPrinter) handleAssistant(event *streamEvent) bool {
	if event.Message == nil {
		return false
	}

	printed := false
	for _, block := range event.Message.Content {
		switch block.Type {
		case ContentTypeToolUse:
			// Blank line before each tool call for readability
			if p.lastPrint != "" {
				fmt.Fprintln(p.w)
			}
			p.printToolUse(block)
			p.lastTool = block.Name
			p.lastPrint = "tool"
			printed = true

		case ContentTypeText:
			if text := strings.TrimSpace(block.Text); text != "" {
				if p.lastPrint != "" {
					fmt.Fprintln(p.w)
				}
				p.printMarkdown(text)
				p.lastPrint = "text"
				printed = true
			}

		case ContentTypeThinking:
			if thinking := strings.TrimSpace(block.Thinking); thinking != "" {
				first := firstLine(thinking)
				if len(first) > 100 {
					first = first[:100] + "..."
				}
				fmt.Fprintln(p.w, thinkStyle.Render(first))
				p.lastPrint = "thinking"
				printed = true
			}

		default:
			fmt.Fprintf(p.w, "%s %s\n", unknownStyle.Render("? Unknown content type:"), dimStyle.Render(block.Type))
			printed = true
		}
	}
	return printed
}

// toolArg extracts the primary argument for display from a tool_use block.
func toolArg(name string, input json.RawMessage) string {
	switch name {
	case "Bash":
		var v struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(input, &v) == nil {
			return truncate(v.Command, 80)
		}
	case "Read", "Write", "Edit":
		var v struct {
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal(input, &v) == nil {
			return shortenPath(v.FilePath)
		}
	case "Glob":
		var v struct {
			Pattern string `json:"pattern"`
		}
		if json.Unmarshal(input, &v) == nil {
			return v.Pattern
		}
	case "Grep":
		var v struct {
			Pattern string `json:"pattern"`
		}
		if json.Unmarshal(input, &v) == nil {
			return v.Pattern
		}
	case "Agent":
		var v struct {
			Description string `json:"description"`
		}
		if json.Unmarshal(input, &v) == nil {
			return v.Description
		}
	}
	return ""
}

// displayName maps tool names to Claude-style display names.
func displayName(name string) string {
	switch name {
	case "Edit":
		return "Update"
	default:
		return name
	}
}

func (p *StreamPrinter) printToolUse(block contentBlock) {
	name := displayName(block.Name)
	arg := toolArg(block.Name, block.Input)

	if arg != "" {
		fmt.Fprintf(p.w, "%s%s(%s)\n", dotStyle.Render(dot), toolStyle.Render(name), argStyle.Render(arg))
	} else {
		fmt.Fprintf(p.w, "%s%s\n", dotStyle.Render(dot), toolStyle.Render(name))
	}
}

// printMarkdown renders text as markdown using glamour, with a ● prefix on the first line.
func (p *StreamPrinter) printMarkdown(text string) {
	if p.md == nil {
		// Fallback if renderer failed to initialize
		fmt.Fprintf(p.w, "%s%s\n", dotTextStyle.Render(dot), textStyle.Render(text))
		return
	}

	rendered, err := p.md.Render(text)
	if err != nil {
		fmt.Fprintf(p.w, "%s%s\n", dotTextStyle.Render(dot), textStyle.Render(text))
		return
	}

	// Trim whitespace glamour adds (leading newlines + trailing whitespace)
	rendered = strings.TrimSpace(rendered)
	if rendered == "" {
		return
	}

	// Add ● prefix to the first line (white dot for text output).
	// Strip blank lines glamour inserts between blocks — we handle spacing ourselves.
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

func (p *StreamPrinter) handleUser(event *streamEvent) bool {
	// Tool results
	if event.ToolUseResult != nil {
		return p.handleToolResult(event)
	}

	// User prompt text (shown with ❯ prefix like Claude)
	if event.Message != nil {
		for _, block := range event.Message.Content {
			if block.Type == ContentTypeText {
				if text := strings.TrimSpace(block.Text); text != "" {
					if p.lastPrint != "" {
						fmt.Fprintln(p.w)
					}
					fmt.Fprintf(p.w, "%s%s\n", dimStyle.Render(userArrow), textStyle.Render(text))
					p.lastPrint = "user"
					return true
				}
			}
		}
	}

	return false
}

func (p *StreamPrinter) handleToolResult(event *streamEvent) bool {
	r := event.ToolUseResult

	// Edit results with structured patch — show diff
	if len(r.StructuredPatch) > 0 {
		p.printEditResult(r.StructuredPatch)
		p.lastPrint = "result"
		return true
	}

	// File read results
	if r.File != nil {
		total := r.File.TotalLines
		if total == 0 {
			total = r.File.NumLines
		}
		fmt.Fprintf(p.w, "%s%s\n", dotOkStyle.Render(result), dimStyle.Render(fmt.Sprintf("Read %d lines", total)))
		p.lastPrint = "result"
		return true
	}

	// Write/create results
	if r.Type == "create" {
		fmt.Fprintf(p.w, "%s%s\n", dotOkStyle.Render(result), dimStyle.Render("Created file"))
		p.lastPrint = "result"
		return true
	}

	// Stderr (errors)
	if r.Stderr != "" {
		stderr := truncate(strings.TrimSpace(r.Stderr), 200)
		fmt.Fprintf(p.w, "%s%s\n", dotErrStyle.Render(result), errorStyle.Render(stderr))
		p.lastPrint = "result"
		return true
	}

	// Stdout preview
	if r.Stdout != "" {
		out := truncate(strings.TrimSpace(r.Stdout), 200)
		fmt.Fprintf(p.w, "%s%s\n", dotOkStyle.Render(result), dimStyle.Render(out))
		p.lastPrint = "result"
		return true
	}

	// Empty result (e.g. Glob, Grep, Bash with no output)
	fmt.Fprintf(p.w, "%s%s\n", dotOkStyle.Render(result), dimStyle.Render("(No output)"))
	p.lastPrint = "result"
	return true
}

func (p *StreamPrinter) printEditResult(patches []patchEntry) {
	added, removed := 0, 0
	var diffLines []string

	for _, patch := range patches {
		for _, line := range patch.Lines {
			switch line.Type {
			case "add":
				added++
				num := fmt.Sprintf("%4d ", line.NewNum)
				diffLines = append(diffLines, lineNumStyle.Render(num)+addStyle.Render("+"+line.Content))
			case "remove":
				removed++
				num := fmt.Sprintf("%4d ", line.OldNum)
				diffLines = append(diffLines, lineNumStyle.Render(num)+removeStyle.Render("-"+line.Content))
			case "context":
				num := fmt.Sprintf("%4d ", line.NewNum)
				diffLines = append(diffLines, lineNumStyle.Render(num)+dimStyle.Render(" "+line.Content))
			}
		}
	}

	// Summary line
	parts := []string{}
	if added > 0 {
		parts = append(parts, fmt.Sprintf("Added %d lines", added))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("removed %d lines", removed))
	}
	summary := strings.Join(parts, ", ")
	if summary == "" {
		summary = "No changes"
	}
	fmt.Fprintf(p.w, "%s%s\n", dotOkStyle.Render(result), dimStyle.Render(summary))

	// Show diff lines (truncated)
	shown := 0
	for _, dl := range diffLines {
		if shown >= maxDiffLines {
			remaining := len(diffLines) - shown
			fmt.Fprintf(p.w, "      %s\n", dimStyle.Render(fmt.Sprintf("... %d more lines", remaining)))
			break
		}
		fmt.Fprintf(p.w, "      %s\n", dl)
		shown++
	}
}

func (p *StreamPrinter) handleResult(event *streamEvent) bool {
	if p.lastPrint == "tool" || p.lastPrint == "result" {
		fmt.Fprintln(p.w)
	}

	if event.IsError {
		fmt.Fprintf(p.w, "%s\n", errorStyle.Render("✗ Error: "+truncate(event.Result, 200)))
	} else {
		fmt.Fprintf(p.w, "%s %s\n", resultStyle.Render("✓ Done"), dimStyle.Render(fmt.Sprintf("(%d turns, %dms, $%.4f)", event.NumTurns, event.DurationMs, event.TotalCostUSD)))
	}
	p.lastPrint = "result"
	return true
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// shortenPath returns a shorter display path by using only the last 2 components.
func shortenPath(path string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	parent := filepath.Base(dir)
	if parent == "." || parent == "/" {
		return base
	}
	return parent + "/" + base
}

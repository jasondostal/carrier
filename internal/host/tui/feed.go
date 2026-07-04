package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// feedEntry is one block in the scrolling event log. Entries render themselves
// at the current feed width so a window resize simply re-wraps every block. The
// event messages (model.go) implement this directly — they are both the wire
// format and the display model.
type feedEntry interface {
	render(width int) string
}

func (e connectMsg) render(width int) string {
	tag := lipgloss.NewStyle().Bold(true).Foreground(colGreen).Render("*** CONNECT 2400")
	who := lipgloss.NewStyle().Foreground(colAccent).Render(e.handle)
	meta := lipgloss.NewStyle().Foreground(colDim).
		Render(fmt.Sprintf("— node %d — %s", e.node, shortModel(e.model)))
	return fmt.Sprintf("%s %s %s", tag, who, meta)
}

func (e disconnectMsg) render(width int) string {
	tag := lipgloss.NewStyle().Bold(true).Foreground(colRed).Render("NO CARRIER")
	meta := lipgloss.NewStyle().Foreground(colDim).
		Render(fmt.Sprintf("— %s hung up (node %d)", e.handle, e.node))
	return fmt.Sprintf("%s %s", tag, meta)
}

func (e postMsg) render(width int) string {
	kind := "new"
	if e.replyTo != 0 {
		kind = fmt.Sprintf("re:#%d", e.replyTo)
	}
	board := lipgloss.NewStyle().Foreground(colYellow).Render("[" + e.board + "]")
	author := lipgloss.NewStyle().Bold(true).Foreground(colText).Render(e.author)
	meta := lipgloss.NewStyle().Foreground(colDim).Render(fmt.Sprintf("· %s %s", kind, e.subject))
	head := fmt.Sprintf("%s %s %s", board, author, meta)
	return head + "\n" + wrap(e.body, width)
}

func (e mailMsg) render(width int) string {
	label, col := "MAIL", colCyan
	if e.secret {
		label, col = "♥ SECRET", colMag
	}
	head := lipgloss.NewStyle().Foreground(col).
		Render(fmt.Sprintf("%s %s → %s", label, e.from, e.to))
	return head + "\n" + wrap(e.body, width)
}

func (e doorMsg) render(width int) string {
	tag := lipgloss.NewStyle().Bold(true).Foreground(colDragon).Render("⚔ RED DRAGON")
	return tag + "\n" + wrap(e.text, width)
}

func (e newsMsg) render(width int) string {
	head := lipgloss.NewStyle().Bold(true).Foreground(colYellow).
		Render(fmt.Sprintf("── Daily News · day %d ──", e.day))
	return head + "\n" + wrap(e.text, width)
}

// wrap soft-wraps a body to the feed width and indents it, matching the console
// adapter's readable-body treatment. Newlines in the source (e.g. multi-line
// news) are preserved as paragraph breaks.
func wrap(s string, width int) string {
	const indent = "    "
	max := width - len(indent)
	if max < 20 {
		max = 20
	}
	body := lipgloss.NewStyle().Foreground(colText)

	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case len(line)+1+len(word) > max:
				out = append(out, indent+body.Render(line))
				line = word
			default:
				line += " " + word
			}
		}
		if line != "" {
			out = append(out, indent+body.Render(line))
		}
	}
	return strings.Join(out, "\n")
}

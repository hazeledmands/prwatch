package ui

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// renderMarkdown converts markdown text to ANSI-formatted terminal text.
// Uses goldmark for parsing and a custom ANSI renderer for output.
func renderMarkdown(md string, width int) (string, error) {
	if width <= 0 {
		width = 80
	}

	source := []byte(md)
	parser := goldmark.DefaultParser()
	doc := parser.Parse(text.NewReader(source))

	var buf bytes.Buffer
	r := &ansiRenderer{
		source: source,
		width:  width,
		buf:    &buf,
	}
	r.render(doc)

	// Trim trailing whitespace but keep structure
	result := strings.TrimRight(buf.String(), "\n")
	return result, nil
}

// ansiRenderer walks a goldmark AST and emits ANSI-styled text.
type ansiRenderer struct {
	source []byte
	width  int
	buf    *bytes.Buffer
	// State tracking
	listDepth   int
	listCounter int // for ordered lists; 0 = unordered
	inCodeBlock bool
}

const (
	ansiBold      = "\x1b[1m"
	ansiItalic    = "\x1b[3m"
	ansiCodeFg    = "\x1b[38;2;166;227;161m" // green for inline code
	ansiCodeBg    = "\x1b[48;2;40;40;40m"
	ansiHeadingFg = "\x1b[38;2;137;220;235m" // cyan for headings
	ansiLinkFg    = "\x1b[38;2;137;180;250m" // blue for links
	mdDimFg       = "\x1b[38;2;136;136;136m" // dim for blockquote
	ansiHRFg      = "\x1b[38;2;85;85;85m"    // dim for HR
	ansiReset     = "\x1b[0m"
)

func (r *ansiRenderer) render(node ast.Node) {
	r.walkNode(node)
}

func (r *ansiRenderer) walkNode(node ast.Node) {
	switch n := node.(type) {
	case *ast.Document:
		r.walkChildren(node)

	case *ast.Heading:
		r.buf.WriteString(ansiHeadingFg + ansiBold)
		prefix := strings.Repeat("#", n.Level) + " "
		r.buf.WriteString(prefix)
		r.walkChildren(node)
		r.buf.WriteString(ansiReset)
		r.buf.WriteString("\n\n")

	case *ast.Paragraph:
		if r.inCodeBlock {
			r.walkChildren(node)
			return
		}
		r.walkChildren(node)
		r.buf.WriteString("\n\n")

	case *ast.TextBlock:
		r.walkChildren(node)
		r.buf.WriteString("\n")

	case *ast.Text:
		r.buf.Write(n.Segment.Value(r.source))
		if n.SoftLineBreak() {
			r.buf.WriteString(" ")
		}
		if n.HardLineBreak() {
			r.buf.WriteString("\n")
		}

	case *ast.String:
		r.buf.Write(n.Value)

	case *ast.Emphasis:
		if n.Level == 2 {
			r.buf.WriteString(ansiBold)
		} else {
			r.buf.WriteString(ansiItalic)
		}
		r.walkChildren(node)
		r.buf.WriteString(ansiReset)

	case *ast.CodeSpan:
		r.buf.WriteString(ansiCodeBg + ansiCodeFg)
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			if t, ok := child.(*ast.Text); ok {
				r.buf.Write(t.Segment.Value(r.source))
			}
		}
		r.buf.WriteString(ansiReset)

	case *ast.FencedCodeBlock:
		r.inCodeBlock = true
		r.buf.WriteString(ansiCodeBg + ansiCodeFg)
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			r.buf.Write(seg.Value(r.source))
		}
		r.buf.WriteString(ansiReset)
		r.buf.WriteString("\n")
		r.inCodeBlock = false

	case *ast.CodeBlock:
		r.inCodeBlock = true
		r.buf.WriteString(ansiCodeBg + ansiCodeFg)
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			r.buf.Write(seg.Value(r.source))
		}
		r.buf.WriteString(ansiReset)
		r.buf.WriteString("\n")
		r.inCodeBlock = false

	case *ast.Link:
		r.buf.WriteString(ansiLinkFg)
		r.walkChildren(node)
		r.buf.WriteString(ansiReset)
		if len(n.Destination) > 0 {
			r.buf.WriteString(mdDimFg)
			r.buf.WriteString(" (")
			r.buf.Write(n.Destination)
			r.buf.WriteString(")")
			r.buf.WriteString(ansiReset)
		}

	case *ast.AutoLink:
		r.buf.WriteString(ansiLinkFg)
		r.buf.Write(n.URL(r.source))
		r.buf.WriteString(ansiReset)

	case *ast.Image:
		r.buf.WriteString("[image: ")
		r.walkChildren(node)
		r.buf.WriteString("]")

	case *ast.List:
		savedCounter := r.listCounter
		if n.IsOrdered() {
			r.listCounter = n.Start
		} else {
			r.listCounter = 0
		}
		r.listDepth++
		r.walkChildren(node)
		r.listDepth--
		r.listCounter = savedCounter

	case *ast.ListItem:
		indent := strings.Repeat("  ", r.listDepth-1)
		if r.listCounter > 0 {
			r.buf.WriteString(fmt.Sprintf("%s%d. ", indent, r.listCounter))
			r.listCounter++
		} else {
			r.buf.WriteString(indent + "• ")
		}
		// Render children inline (avoiding extra paragraph newlines)
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			if _, ok := child.(*ast.Paragraph); ok {
				// Render paragraph contents inline within list item
				r.walkChildren(child)
			} else {
				r.walkNode(child)
			}
		}
		r.buf.WriteString("\n")

	case *ast.Blockquote:
		// Capture blockquote content, then prefix each line with ▌
		var inner bytes.Buffer
		savedBuf := r.buf
		r.buf = &inner
		r.walkChildren(node)
		r.buf = savedBuf

		for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
			r.buf.WriteString(mdDimFg + "▌ " + ansiReset + line + "\n")
		}
		r.buf.WriteString("\n")

	case *ast.ThematicBreak:
		hr := strings.Repeat("─", min(r.width, 40))
		r.buf.WriteString(ansiHRFg + hr + ansiReset + "\n\n")

	case *ast.HTMLBlock:
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			r.buf.Write(seg.Value(r.source))
		}
		r.buf.WriteString("\n")

	case *ast.RawHTML:
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			r.buf.Write(seg.Value(r.source))
		}

	default:
		// For any unhandled node types, just walk children
		r.walkChildren(node)
	}
}

func (r *ansiRenderer) walkChildren(node ast.Node) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		r.walkNode(child)
	}
}

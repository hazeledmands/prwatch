package ui

import (
	"strings"
	"testing"
)

func TestRenderMarkdown_Headings(t *testing.T) {
	md := "# Heading 1\n\n## Heading 2\n\nSome text."
	out := renderMarkdown(md, 80)
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "# Heading 1") {
		t.Errorf("expected heading 1, got: %q", stripped)
	}
	if !strings.Contains(stripped, "## Heading 2") {
		t.Errorf("expected heading 2, got: %q", stripped)
	}
	if !strings.Contains(stripped, "Some text.") {
		t.Errorf("expected body text, got: %q", stripped)
	}
	// Headings should have ANSI bold
	if !strings.Contains(out, ansiBold) {
		t.Error("headings should contain bold ANSI")
	}
}

func TestRenderMarkdown_Bold(t *testing.T) {
	md := "This is **bold** text."
	out := renderMarkdown(md, 80)
	if !strings.Contains(out, ansiBold) {
		t.Error("bold text should contain bold ANSI code")
	}
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "bold") {
		t.Errorf("expected 'bold' in output, got: %q", stripped)
	}
}

func TestRenderMarkdown_Italic(t *testing.T) {
	md := "This is *italic* text."
	out := renderMarkdown(md, 80)
	if !strings.Contains(out, ansiItalic) {
		t.Error("italic text should contain italic ANSI code")
	}
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	md := "```go\nfmt.Println(\"hello\")\n```"
	out := renderMarkdown(md, 80)
	if !strings.Contains(out, ansiCodeFg) {
		t.Error("code block should contain code color ANSI")
	}
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "fmt.Println") {
		t.Errorf("expected code content, got: %q", stripped)
	}
}

func TestRenderMarkdown_InlineCode(t *testing.T) {
	md := "Use `go test` to run."
	out := renderMarkdown(md, 80)
	if !strings.Contains(out, ansiCodeFg) {
		t.Error("inline code should contain code color ANSI")
	}
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "go test") {
		t.Errorf("expected inline code content, got: %q", stripped)
	}
}

func TestRenderMarkdown_List(t *testing.T) {
	md := "- item one\n- item two\n- item three"
	out := renderMarkdown(md, 80)
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "• item one") {
		t.Errorf("expected bullet list item, got: %q", stripped)
	}
	if !strings.Contains(stripped, "• item two") {
		t.Errorf("expected bullet list item two, got: %q", stripped)
	}
}

func TestRenderMarkdown_OrderedList(t *testing.T) {
	md := "1. first\n2. second\n3. third"
	out := renderMarkdown(md, 80)
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "1. first") {
		t.Errorf("expected ordered list, got: %q", stripped)
	}
	if !strings.Contains(stripped, "2. second") {
		t.Errorf("expected second ordered item, got: %q", stripped)
	}
}

func TestRenderMarkdown_Link(t *testing.T) {
	md := "See [docs](https://example.com)."
	out := renderMarkdown(md, 80)
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "docs") {
		t.Errorf("expected link text, got: %q", stripped)
	}
	if !strings.Contains(stripped, "https://example.com") {
		t.Errorf("expected link URL, got: %q", stripped)
	}
}

func TestRenderMarkdown_Blockquote(t *testing.T) {
	md := "> This is a quote."
	out := renderMarkdown(md, 80)
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "▌") {
		t.Errorf("expected blockquote indicator, got: %q", stripped)
	}
	if !strings.Contains(stripped, "This is a quote.") {
		t.Errorf("expected quote text, got: %q", stripped)
	}
}

func TestRenderMarkdown_HorizontalRule(t *testing.T) {
	md := "Before\n\n---\n\nAfter"
	out := renderMarkdown(md, 80)
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "───") {
		t.Errorf("expected horizontal rule, got: %q", stripped)
	}
}

func TestRenderMarkdown_EmptyInput(t *testing.T) {
	out := renderMarkdown("", 80)
	if out != "" {
		t.Errorf("expected empty output for empty input, got: %q", out)
	}
}

// HTML cleanup: PR descriptions and comments often contain raw HTML
// (Linear-style <details>/<summary>, <br> for line breaks, HTML comments).
// These get rendered as raw tags by goldmark's default renderer, which is
// noisy in a TUI. The preprocessor below cleans up common cases.

func TestRenderMarkdown_BrTagBecomesNewline(t *testing.T) {
	for _, md := range []string{
		"line one<br>line two",
		"line one<br/>line two",
		"line one<br />line two",
		"line one<BR>line two",
	} {
		out := renderMarkdown(md, 80)
		stripped := stripANSI(out)
		if strings.Contains(stripped, "<br") || strings.Contains(stripped, "<BR") {
			t.Errorf("%q: <br> tag should not appear in output, got %q", md, stripped)
		}
		// Both lines should be present.
		if !strings.Contains(stripped, "line one") || !strings.Contains(stripped, "line two") {
			t.Errorf("%q: both lines should be in output, got %q", md, stripped)
		}
		// They should be on separate lines.
		idx1 := strings.Index(stripped, "line one")
		idx2 := strings.Index(stripped, "line two")
		between := stripped[idx1:idx2]
		if !strings.Contains(between, "\n") {
			t.Errorf("%q: lines should be separated by newline, got %q", md, stripped)
		}
	}
}

func TestRenderMarkdown_HTMLCommentsStripped(t *testing.T) {
	md := "before<!-- this is a comment -->after"
	out := renderMarkdown(md, 80)
	stripped := stripANSI(out)
	if strings.Contains(stripped, "<!--") || strings.Contains(stripped, "-->") {
		t.Errorf("HTML comment markers should be stripped, got %q", stripped)
	}
	if strings.Contains(stripped, "this is a comment") {
		t.Errorf("HTML comment content should be stripped, got %q", stripped)
	}
	if !strings.Contains(stripped, "before") || !strings.Contains(stripped, "after") {
		t.Errorf("non-comment text should remain, got %q", stripped)
	}
}

func TestRenderMarkdown_HTMLBlockCommentStripped(t *testing.T) {
	// HTML comments often appear as block-level constructs.
	md := "before\n\n<!-- a comment -->\n\nafter"
	out := renderMarkdown(md, 80)
	stripped := stripANSI(out)
	if strings.Contains(stripped, "<!--") || strings.Contains(stripped, "-->") {
		t.Errorf("HTML comment markers should be stripped, got %q", stripped)
	}
	if strings.Contains(stripped, "a comment") {
		t.Errorf("HTML comment content should be stripped, got %q", stripped)
	}
}

func TestRenderMarkdown_DetailsSummaryExpanded(t *testing.T) {
	md := "<details>\n<summary>Click to expand</summary>\n\nHidden body content here.\n\n</details>"
	out := renderMarkdown(md, 80)
	stripped := stripANSI(out)
	if strings.Contains(stripped, "<details") || strings.Contains(stripped, "</details") {
		t.Errorf("details tags should be stripped, got %q", stripped)
	}
	if strings.Contains(stripped, "<summary") || strings.Contains(stripped, "</summary") {
		t.Errorf("summary tags should be stripped, got %q", stripped)
	}
	if !strings.Contains(stripped, "Click to expand") {
		t.Errorf("summary text should be rendered, got %q", stripped)
	}
	if !strings.Contains(stripped, "Hidden body content here.") {
		t.Errorf("inner body content should be rendered, got %q", stripped)
	}
}

func TestRenderMarkdown_StripsUnknownInlineTags(t *testing.T) {
	md := "before <span>middle</span> after"
	out := renderMarkdown(md, 80)
	stripped := stripANSI(out)
	if strings.Contains(stripped, "<span") || strings.Contains(stripped, "</span") {
		t.Errorf("span tags should be stripped, got %q", stripped)
	}
	if !strings.Contains(stripped, "middle") {
		t.Errorf("inner text should remain, got %q", stripped)
	}
}

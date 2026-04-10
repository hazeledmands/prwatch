package ui

import (
	"strings"
	"testing"
)

func TestRenderMarkdown_Headings(t *testing.T) {
	md := "# Heading 1\n\n## Heading 2\n\nSome text."
	out, err := renderMarkdown(md, 80)
	if err != nil {
		t.Fatal(err)
	}
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
	out, err := renderMarkdown(md, 80)
	if err != nil {
		t.Fatal(err)
	}
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
	out, err := renderMarkdown(md, 80)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, ansiItalic) {
		t.Error("italic text should contain italic ANSI code")
	}
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	md := "```go\nfmt.Println(\"hello\")\n```"
	out, err := renderMarkdown(md, 80)
	if err != nil {
		t.Fatal(err)
	}
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
	out, err := renderMarkdown(md, 80)
	if err != nil {
		t.Fatal(err)
	}
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
	out, err := renderMarkdown(md, 80)
	if err != nil {
		t.Fatal(err)
	}
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
	out, err := renderMarkdown(md, 80)
	if err != nil {
		t.Fatal(err)
	}
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
	out, err := renderMarkdown(md, 80)
	if err != nil {
		t.Fatal(err)
	}
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
	out, err := renderMarkdown(md, 80)
	if err != nil {
		t.Fatal(err)
	}
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
	out, err := renderMarkdown(md, 80)
	if err != nil {
		t.Fatal(err)
	}
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "───") {
		t.Errorf("expected horizontal rule, got: %q", stripped)
	}
}

func TestRenderMarkdown_EmptyInput(t *testing.T) {
	out, err := renderMarkdown("", 80)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("expected empty output for empty input, got: %q", out)
	}
}

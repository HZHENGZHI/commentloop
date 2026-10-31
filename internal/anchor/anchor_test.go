package anchor

import (
	"strings"
	"testing"
)

func TestLocateQuote(t *testing.T) {
	const content = "intro\n\nalpha beta\n\ngamma beta\n\ndone"
	tests := []struct {
		name      string
		quote     string
		prefix    string
		suffix    string
		nearLine  int
		wantStart int
		wantLine  int
		orphaned  bool
	}{
		{name: "exact match", quote: "alpha beta", wantStart: 7, wantLine: 3},
		{name: "duplicate chooses nearest line", quote: "beta", nearLine: 5, wantStart: 25, wantLine: 5},
		{name: "duplicate chooses nearest line above", quote: "beta", nearLine: 3, wantStart: 13, wantLine: 3},
		{name: "prefix outweighs line distance", quote: "beta", prefix: "alpha ", nearLine: 5, wantStart: 13, wantLine: 3},
		{name: "suffix outweighs line distance", quote: "beta", suffix: "\n\ndone", nearLine: 3, wantStart: 25, wantLine: 5},
		{name: "missing quote is orphaned", quote: "not here", orphaned: true},
		{name: "empty quote is orphaned", quote: "", orphaned: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end, line, orphaned := LocateQuote(content, test.quote, test.prefix, test.suffix, test.nearLine)
			if orphaned != test.orphaned {
				t.Fatalf("orphaned = %v, want %v", orphaned, test.orphaned)
			}
			if orphaned {
				if start != 0 || end != 0 || line != 0 {
					t.Fatalf("orphaned result = (%d, %d, %d), want zeroes", start, end, line)
				}
				return
			}
			if start != test.wantStart || line != test.wantLine {
				t.Fatalf("start/line = %d/%d, want %d/%d", start, line, test.wantStart, test.wantLine)
			}
			if want := content[start : start+len(test.quote)]; want != test.quote {
				t.Fatalf("content[start:end] = %q, want %q", want, test.quote)
			}
			if end != start+len(test.quote) {
				t.Fatalf("end = %d, want %d", end, start+len(test.quote))
			}
		})
	}
}

func TestLocateQuoteMultibyte(t *testing.T) {
	const content = "第一段\n\n第二段有评论\n\n第三段"
	start, end, line, orphaned := LocateQuote(content, "第二段有评论", "", "", 0)
	if orphaned {
		t.Fatal("expected the quote to be located")
	}
	if line != 3 || content[start:end] != "第二段有评论" {
		t.Fatalf("got start=%d end=%d line=%d", start, end, line)
	}
}

func TestLocateQuoteEmptyContent(t *testing.T) {
	if _, _, _, orphaned := LocateQuote("", "quote", "", "", 0); !orphaned {
		t.Fatal("empty content must report the quote as orphaned")
	}
}

func TestLocateCompact(t *testing.T) {
	const content = "## Hello **world**\n\nAgain: Hello world"
	if line, ok := LocateCompact(content, "Hello world", 1); !ok || line != 1 {
		t.Fatalf("line = %d, ok = %v, want 1, true", line, ok)
	}
	if line, ok := LocateCompact(content, "Hello world", 3); !ok || line != 3 {
		t.Fatalf("near line 3: line = %d, ok = %v, want 3, true", line, ok)
	}
	if _, ok := LocateCompact(content, "a", 0); ok {
		t.Fatal("a one-character quote must not match in compact mode")
	}
	if _, ok := LocateCompact(content, "tuesday", 0); ok {
		t.Fatal("missing quote must not match")
	}
}

func TestSectionRange(t *testing.T) {
	const content = "alpha\nbeta\n\ngamma\ndelta\n\nomega"
	tests := []struct {
		name   string
		marker string
		quote  string
		line   int
		want   string
		ok     bool
	}{
		{name: "marker selects paragraph", marker: "gamma", want: "gamma\ndelta", ok: true},
		{name: "line selects paragraph", line: 5, want: "gamma\ndelta", ok: true},
		{name: "blank line snaps to nearest paragraph", line: 3, want: "gamma\ndelta", ok: true},
		{name: "line past the end clamps", line: 99, want: "omega", ok: true},
		{name: "quote fallback", quote: "alpha", want: "alpha\nbeta", ok: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end, ok := SectionRange(content, test.marker, test.quote, test.line)
			if ok != test.ok {
				t.Fatalf("ok = %v, want %v", ok, test.ok)
			}
			if !ok {
				return
			}
			if got := content[start:end]; got != test.want {
				t.Fatalf("section = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSectionRangeEmptyContent(t *testing.T) {
	if _, _, ok := SectionRange("", "anything", "", 1); ok {
		t.Fatal("empty content must not produce a section")
	}
}

func TestPlainText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "strips inline formatting",
			raw:  "# Heading\n\nSome **bold** and [link](https://example.com) text.\n\n![pic](a.png)",
			want: "Some **bold** and link text.",
		},
		{
			name: "skips fenced code lines",
			raw:  "```go\nfmt.Println()\n```\n\nreal text here",
			want: "real text here",
		},
		{
			name: "strips list markers and html",
			raw:  "- <b>item</b> one",
			want: "item one",
		},
		{
			name: "picks the longest paragraph",
			raw:  "short\n\nmuch longer paragraph\n\nshort too",
			want: "much longer paragraph",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PlainText(test.raw); got != test.want {
				t.Fatalf("PlainText() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPlainTextCapsLongParagraph(t *testing.T) {
	got := PlainText(strings.Repeat("字", 450))
	if count := len([]rune(got)); count != 400 {
		t.Fatalf("rune count = %d, want 400", count)
	}
}

func TestPlainTextEmpty(t *testing.T) {
	if got := PlainText("   \n\n  "); got != "" {
		t.Fatalf("PlainText() = %q, want empty", got)
	}
}

func TestReplaceSection(t *testing.T) {
	const content = "a\n\nb\n\nb"
	if got, ok := ReplaceSection(content, "b", "B", 3); !ok || got != "a\n\nB\n\nb" {
		t.Fatalf("first block: got %q, ok %v", got, ok)
	}
	if got, ok := ReplaceSection(content, "b", "B", 5); !ok || got != "a\n\nb\n\nB" {
		t.Fatalf("nearest block: got %q, ok %v", got, ok)
	}
	if got, ok := ReplaceSection(content, "missing", "B", 3); ok || got != content {
		t.Fatalf("missing block: got %q, ok %v", got, ok)
	}
	if got, ok := ReplaceSection(content, "", "B", 3); ok || got != content {
		t.Fatalf("empty block: got %q, ok %v", got, ok)
	}
}

func TestSectionFromVersion(t *testing.T) {
	const content = "alpha\nbeta\n\ngamma\ndelta"
	if got, ok := SectionFromVersion(content, "", 12, 23, "", 0); !ok || got != "gamma\ndelta" {
		t.Fatalf("stored offsets: got %q, ok %v", got, ok)
	}
	if got, ok := SectionFromVersion(content, "gamma", -1, -1, "", 0); !ok || got != "gamma\ndelta" {
		t.Fatalf("fallback: got %q, ok %v", got, ok)
	}
	if _, ok := SectionFromVersion("", "missing", 0, 0, "", 0); ok {
		t.Fatal("empty content must not produce a section")
	}
}

func TestSectionFromVersionTrimsLineEndings(t *testing.T) {
	const content = "alpha\n\nbeta\r\n"
	got, ok := SectionFromVersion(content, "", 7, 13, "", 0)
	if !ok || got != "beta" {
		t.Fatalf("got %q, ok %v, want beta", got, ok)
	}
}

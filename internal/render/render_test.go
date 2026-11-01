package render

import (
	"strings"
	"testing"
)

func TestRenderGFMAndHighlighting(t *testing.T) {
	r := New()
	src := "# 标题\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n- [x] 完成\n\n```go\nfmt.Println(\"hi\")\n```\n"
	got := r.Render([]byte(src), "notes/demo.md")

	for _, want := range []string{
		"<h1",
		"<table>",
		`type="checkbox"`,
		`class="chroma"`,
		"标题",
	} {
		if !strings.Contains(got.HTML, want) {
			t.Errorf("rendered HTML missing %q\n%s", want, got.HTML)
		}
	}
	if len(got.Revision) != 16 {
		t.Errorf("revision = %q, want 16 hex chars", got.Revision)
	}
}

func TestRenderSafeDropsRawHTML(t *testing.T) {
	r := New()
	src := "<script>alert(1)</script>\n\n**safe**\n"
	got := r.RenderSafe([]byte(src), "notes/demo.md")

	if strings.Contains(got.HTML, "<script") {
		t.Fatalf("safe render kept raw HTML: %s", got.HTML)
	}
	if !strings.Contains(got.HTML, "<strong>safe</strong>") {
		t.Fatalf("safe render lost Markdown formatting: %s", got.HTML)
	}
	if !strings.Contains(r.Render([]byte(src), "notes/demo.md").HTML, "<script") {
		t.Fatal("unsafe preview no longer allows raw HTML; update the trust model intentionally")
	}
}

func TestRevisionChanges(t *testing.T) {
	r := New()
	a := r.Render([]byte("hello"), "a.md")
	b := r.Render([]byte("hello"), "b.md")
	c := r.Render([]byte("hello!"), "a.md")
	if a.Revision == b.Revision {
		t.Error("revision should depend on the document path")
	}
	if a.Revision == c.Revision {
		t.Error("revision should depend on the content")
	}
}

func TestRelativeImagesRewritten(t *testing.T) {
	r := New()
	src := strings.Join([]string{
		"![one](pic.png)",
		"![two](./img/a%20b.png)",
		"![three](https://example.com/x.png)",
		"![four](/static/x.png)",
		"![five](../up.png)",
	}, "\n")
	got := r.Render([]byte(src), "notes/sub/doc.md").HTML

	for _, want := range []string{
		`src="/raw/notes/sub/pic.png"`,
		`src="/raw/notes/sub/img/a%20b.png"`,
		`src="https://example.com/x.png"`,
		`src="/static/x.png"`,
		`src="/raw/notes/up.png"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestHighlightCSSBothThemes(t *testing.T) {
	r := New()
	light := r.HighlightCSS("light")
	dark := r.HighlightCSS("dark")
	if !strings.Contains(light, ".chroma") || !strings.Contains(dark, ".chroma") {
		t.Fatal("expected chroma classes in generated CSS")
	}
	if light == dark {
		t.Fatal("light and dark CSS should differ")
	}
	if !strings.Contains(light, "background: transparent") {
		t.Fatal("expected transparent code backgrounds")
	}
}

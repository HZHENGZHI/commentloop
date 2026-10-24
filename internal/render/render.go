// Package render turns Markdown source into HTML with GitHub-flavored
// extensions and Chroma syntax highlighting.
package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	stdhtml "html"
	"net/url"
	"path"
	"strings"
	"sync"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	rendererhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Result is the rendered output for one document revision.
type Result struct {
	HTML     string
	Revision string
}

// Renderer is safe for concurrent use.
type Renderer struct {
	md  goldmark.Markdown
	css sync.Map // style name -> generated CSS
}

var docPathKey = parser.NewContextKey()

// New builds a renderer configured for mdlive's preview.
func New() *Renderer {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.Typographer,
			highlighting.NewHighlighting(
				highlighting.WithStyle("github"),
				highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(
				util.Prioritized(imageRewriter{}, 100),
			),
		),
		goldmark.WithRendererOptions(
			// Notes are local and user-authored; raw HTML is expected.
			rendererhtml.WithUnsafe(),
		),
	)
	return &Renderer{md: md}
}

// Render renders source and returns a content revision derived from the
// source plus the document path (the path influences relative image URLs).
func (r *Renderer) Render(source []byte, docPath string) Result {
	ctx := parser.NewContext()
	ctx.Set(docPathKey, docPath)

	doc := r.md.Parser().Parse(text.NewReader(source), parser.WithContext(ctx))

	var buf bytes.Buffer
	if err := r.md.Renderer().Render(&buf, source, doc); err != nil {
		// goldmark's renderer only fails on writer errors, but keep the
		// preview usable if that ever happens.
		escaped := stdhtml.EscapeString(string(source))
		return Result{HTML: "<pre>" + escaped + "</pre>", Revision: revision(source, docPath)}
	}
	return Result{HTML: buf.String(), Revision: revision(source, docPath)}
}

// HighlightCSS returns Chroma CSS for the requested theme ("dark" or light).
func (r *Renderer) HighlightCSS(theme string) string {
	styleName := "github"
	if theme == "dark" {
		styleName = "github-dark"
	}
	if cached, ok := r.css.Load(styleName); ok {
		return cached.(string)
	}

	style := styles.Get(styleName)
	if style == nil {
		style = styles.Get("monokai")
	}
	if style == nil {
		return ""
	}
	var buf bytes.Buffer
	formatter := chromahtml.New(chromahtml.WithClasses(true))
	if err := formatter.WriteCSS(&buf, style); err != nil {
		return ""
	}
	css := buf.String() + "\n/* mdlive: let the preview surface show through */\n" +
		"pre.chroma, .chroma { background: transparent !important; }\n"
	r.css.Store(styleName, css)
	return css
}

func revision(source []byte, docPath string) string {
	h := sha256.New()
	h.Write(source)
	h.Write([]byte{0})
	h.Write([]byte(docPath))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// imageRewriter resolves relative image destinations against the document
// directory and serves them through /raw/.
type imageRewriter struct{}

func (imageRewriter) Transform(doc *ast.Document, _ text.Reader, pc parser.Context) {
	rawPath, _ := pc.Get(docPathKey).(string)
	dir := path.Dir(rawPath)
	if dir == "." {
		dir = ""
	}

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		img, ok := n.(*ast.Image)
		if !ok {
			return ast.WalkContinue, nil
		}
		dest := string(img.Destination)
		if !isRelativeAsset(dest) {
			return ast.WalkContinue, nil
		}
		u, err := url.Parse(dest)
		if err != nil {
			return ast.WalkContinue, nil
		}
		joined := u.Path
		if dir != "" {
			joined = path.Join(dir, joined)
		}
		joined = strings.TrimPrefix(path.Clean(joined), "/")
		raw := (&url.URL{Path: "/raw/" + joined}).String()
		if u.RawQuery != "" {
			raw += "?" + u.RawQuery
		}
		if u.Fragment != "" {
			raw += "#" + u.Fragment
		}
		img.Destination = []byte(raw)
		return ast.WalkContinue, nil
	})
}

func isRelativeAsset(dest string) bool {
	if dest == "" || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "/") ||
		strings.HasPrefix(dest, "//") {
		return false
	}
	u, err := url.Parse(dest)
	if err != nil {
		return false
	}
	return u.Scheme == ""
}

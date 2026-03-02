package gui

import (
	"bytes"
	"fmt"
	gohtml "html"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// WikiLinkNode represents a [[wikilink]] in the AST.
var kindWikiLink = ast.NewNodeKind("WikiLink")

type WikiLinkNode struct {
	ast.BaseInline
	Target  string // the note name/path inside [[ ]]
	Display string // optional display text after |
}

func (n *WikiLinkNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{
		"Target":  n.Target,
		"Display": n.Display,
	}, nil)
}

func (n *WikiLinkNode) Kind() ast.NodeKind { return kindWikiLink }

type wikiLinkParser struct{}

func (p *wikiLinkParser) Trigger() []byte {
	return []byte{'['}
}

func (p *wikiLinkParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 4 || line[0] != '[' || line[1] != '[' {
		return nil
	}

	// Find closing ]]
	end := bytes.Index(line[2:], []byte("]]"))
	if end < 0 {
		return nil
	}

	inner := string(line[2 : 2+end])
	if inner == "" {
		return nil
	}

	block.Advance(end + 4) // skip [[ + content + ]]

	target := inner
	display := inner

	// Handle [[target|display]] syntax
	if idx := strings.Index(inner, "|"); idx > 0 {
		target = inner[:idx]
		display = inner[idx+1:]
	}

	// Security: normalize and prevent path traversal
	target = sanitizeWikiLinkTarget(target)
	if target == "" {
		return nil
	}

	node := &WikiLinkNode{
		Target:  target,
		Display: display,
	}
	return node
}

// sanitizeWikiLinkTarget normalizes the link target to prevent path traversal.
func sanitizeWikiLinkTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}

	// Normalize separators
	target = filepath.ToSlash(target)

	// Remove dangerous components
	parts := strings.Split(target, "/")
	var safe []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "." || p == ".." {
			continue
		}
		safe = append(safe, p)
	}

	if len(safe) == 0 {
		return ""
	}

	return strings.Join(safe, "/")
}

// isImageFile checks if a filename has a common image extension.
func isImageFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".ico", ".tiff":
		return true
	}
	return false
}

// embedRegex matches Obsidian-style ![[filename]] and ![[filename|alt]] embeds.
var embedRegex = regexp.MustCompile(`!\[\[([^\]|]+?)(?:\|([^\]]+?))?\]\]`)

// preprocessEmbeds converts Obsidian-style ![[image.png]] to standard markdown image syntax
// so that Goldmark can render them as <img> tags.
// Uses angle-bracket enclosed URLs to safely handle filenames with spaces.
func preprocessEmbeds(source []byte) []byte {
	return embedRegex.ReplaceAllFunc(source, func(match []byte) []byte {
		sm := embedRegex.FindSubmatch(match)
		if len(sm) < 2 {
			return match
		}
		target := string(sm[1])
		alt := target
		if len(sm) > 2 && len(sm[2]) > 0 {
			alt = string(sm[2])
		}
		if isImageFile(target) {
			// Convert to standard markdown image with angle-bracket URL (CommonMark-safe for spaces)
			return []byte(fmt.Sprintf("![%s](<%s>)", alt, target))
		}
		// Non-image embed: strip the ! and keep as wikilink
		if len(sm) > 2 && len(sm[2]) > 0 {
			return []byte(fmt.Sprintf("[[%s|%s]]", target, string(sm[2])))
		}
		return []byte(fmt.Sprintf("[[%s]]", target))
	})
}

// imgPathSpaceRegex matches standard markdown images with spaces in the URL
// e.g. ![alt](image 17.png) where the URL is NOT wrapped in < >.
var imgPathSpaceRegex = regexp.MustCompile(`(!\[[^\]]*\])\(([^)<>]*\s[^)<>]*)\)`)

// fixImagePathSpaces wraps image URLs containing spaces in angle brackets
// so goldmark can parse them correctly per CommonMark spec.
func fixImagePathSpaces(source []byte) []byte {
	return imgPathSpaceRegex.ReplaceAll(source, []byte("$1(<$2>)"))
}

type wikiLinkHTMLRenderer struct{}

func (r *wikiLinkHTMLRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindWikiLink, r.renderWikiLink)
}

func (r *wikiLinkHTMLRenderer) renderWikiLink(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*WikiLinkNode)

	// Ensure the link has .md extension for resolution
	href := n.Target
	if !strings.HasSuffix(href, ".md") {
		href += ".md"
	}

	_, _ = fmt.Fprintf(w,
		`<a href="#" class="wikilink" data-target="%s">%s</a>`,
		gohtml.EscapeString(href),
		gohtml.EscapeString(n.Display),
	)
	return ast.WalkContinue, nil
}

// ──────────────────────────────────────────────
// Callout AST Transformer
// ──────────────────────────────────────────────

// calloutTransformer transforms blockquotes starting with [!TYPE] into callout divs.
type calloutTransformer struct{}

var calloutRegex = regexp.MustCompile(`^\[!(NOTE|TIP|INFO|IMPORTANT|WARNING|CAUTION|DANGER|BUG|EXAMPLE|QUOTE|SUCCESS|QUESTION|FAILURE|ABSTRACT)\](.*)`)

func (t *calloutTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		bq, ok := node.(*ast.Blockquote)
		if !ok {
			return ast.WalkContinue, nil
		}

		// Get first paragraph text
		firstChild := bq.FirstChild()
		if firstChild == nil {
			return ast.WalkContinue, nil
		}

		para, ok := firstChild.(*ast.Paragraph)
		if !ok {
			return ast.WalkContinue, nil
		}

		// Extract text content of the first line
		var firstLine string
		for c := para.FirstChild(); c != nil; c = c.NextSibling() {
			if txt, ok := c.(*ast.Text); ok {
				firstLine = string(txt.Value(reader.Source()))
				break
			}
		}

		matches := calloutRegex.FindStringSubmatch(firstLine)
		if matches == nil {
			return ast.WalkContinue, nil
		}

		calloutType := strings.ToLower(matches[1])
		calloutTitle := strings.TrimSpace(matches[2])
		if calloutTitle == "" {
			calloutTitle = strings.Title(calloutType)
		}

		// Set attributes on the blockquote to be used in rendering
		bq.SetAttributeString("class", []byte("callout callout-"+calloutType))
		bq.SetAttributeString("data-callout", []byte(calloutType))
		bq.SetAttributeString("data-callout-title", []byte(calloutTitle))

		return ast.WalkContinue, nil
	})
}

// ──────────────────────────────────────────────
// Goldmark Extension
// ──────────────────────────────────────────────

// obsidianExtension bundles wikilinks and callout transformer.
type obsidianExtension struct{}

func (e *obsidianExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(&wikiLinkParser{}, 199),
		),
		parser.WithASTTransformers(
			util.Prioritized(&calloutTransformer{}, 500),
		),
	)
	m.Renderer().AddOptions(
		renderer.WithNodeRenderers(
			util.Prioritized(&wikiLinkHTMLRenderer{}, 500),
		),
	)
}

// ──────────────────────────────────────────────
// Public API
// ──────────────────────────────────────────────

// MarkdownRenderer provides markdown-to-HTML conversion.
type MarkdownRenderer struct {
	md goldmark.Markdown
}

// NewMarkdownRenderer creates a renderer with Obsidian-like extensions.
func NewMarkdownRenderer() *MarkdownRenderer {
	md := goldmark.New(
		goldmark.WithExtensions(
			meta.Meta,
			&obsidianExtension{},
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithAttribute(),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(), // allow raw HTML in markdown
		),
	)
	return &MarkdownRenderer{md: md}
}

// RenderResult holds the rendered HTML and parsed frontmatter.
type RenderResult struct {
	HTML        string                 `json:"html"`
	Frontmatter map[string]interface{} `json:"frontmatter,omitempty"`
}

// Render converts markdown content to HTML with frontmatter parsing.
func (r *MarkdownRenderer) Render(source []byte) (*RenderResult, error) {
	// Preprocess Obsidian-style image embeds: ![[image.png]] → ![image.png](<image.png>)
	source = preprocessEmbeds(source)

	// Fix standard markdown images with spaces in URL: ![alt](path name.png) → ![alt](<path name.png>)
	source = fixImagePathSpaces(source)

	ctx := parser.NewContext()
	var buf bytes.Buffer

	if err := r.md.Convert(source, &buf, parser.WithContext(ctx)); err != nil {
		return nil, fmt.Errorf("markdown render error: %w", err)
	}

	frontmatter := meta.Get(ctx)

	return &RenderResult{
		HTML:        buf.String(),
		Frontmatter: frontmatter,
	}, nil
}

// ExtractWikiLinks extracts all [[wikilink]] targets from markdown source.
func ExtractWikiLinks(source []byte) []string {
	re := regexp.MustCompile(`\[\[([^\]|]+?)(?:\|[^\]]+?)?\]\]`)
	matches := re.FindAllSubmatch(source, -1)

	var links []string
	seen := make(map[string]bool)
	for _, m := range matches {
		target := sanitizeWikiLinkTarget(string(m[1]))
		if target != "" && !seen[target] {
			seen[target] = true
			links = append(links, target)
		}
	}
	return links
}

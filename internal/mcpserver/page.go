package mcpserver

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"

	"docullm/internal/docparse"

	"github.com/gomarkdown/markdown"
)

var reSpace = regexp.MustCompile(`\s+`)

// CleanTitle strips the distro and product names and versions from a document
// title, so the UI can present them as their own categories instead of baking
// them into the title.
func CleanTitle(title string, d docparse.Distro, p docparse.Product) string {
	s := title
	if d.Name != "" {
		s = distroPart(d.Name).ReplaceAllString(s, " ")
	}
	for _, v := range d.Versions {
		s = distroPart(v).ReplaceAllString(s, " ")
	}
	if p.Name != "" {
		s = distroPart(p.Name).ReplaceAllString(s, " ")
	}
	for _, v := range p.Versions {
		s = distroPart(v).ReplaceAllString(s, " ")
	}
	return strings.TrimSpace(reSpace.ReplaceAllString(s, " "))
}

func distroPart(p string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(p) + `\b`)
}

// pages holds the per-tool page templates, one per MCP tool and named after
// it. They live here, next to the tools they render, while the global page
// chrome (header, nav, styles, index) stays in the command's own ui.html shell.
// A page template calls its tool itself (via the web interface's "tool"
// function) and renders the tool's structured output, so the field names in
// the templates are exactly the JSON names an MCP client sees.
//
//go:embed list_documents.html
//go:embed read_document.html
//go:embed search_docs.html
//go:embed find_command.html
//go:embed define_term.html
var pages embed.FS

// PageNames are the page templates in PageTemplates, which are also the names
// of the tools they render.
var PageNames = []string{"list_documents", "read_document", "search_docs", "find_command", "define_term"}

// PageTemplates returns the file system holding the per-tool page templates,
// so the command can parse them into the same template set as its shell.
func PageTemplates() embed.FS { return pages }

// PageFuncs returns the template functions shared by the shell and the page
// templates. esc avoids double-escaping the already-trusted chapter HTML.
// clean and markdown take the JSON-decoded tool output.
func PageFuncs() template.FuncMap {
	return template.FuncMap{
		"esc":  func(s any) template.HTML { return template.HTML(escText(s)) },
		"join": func(ss []string) string { return strings.Join(ss, ", ") },
		"str":  func(v any) string { return fmt.Sprint(v) },
		"clean": func(title string, d, p any) string {
			return CleanTitle(title, decodeAs[docparse.Distro](d), decodeAs[docparse.Product](p))
		},
		// has reports whether id is one of the comma-separated ids in list,
		// for preselecting a multi-select of documents.
		"has": func(list, id string) bool {
			for _, v := range strings.Split(list, ",") {
				if strings.TrimSpace(v) == id {
					return true
				}
			}
			return false
		},
		"markdown": func(md string) template.HTML { return markdownToHTML(md) },
	}
}

// decodeAs converts a JSON-decoded value (e.g. a map from a tool's structured
// output) back into T. Values that do not fit leave T at its zero value.
func decodeAs[T any](v any) T {
	var t T
	if b, err := json.Marshal(v); err == nil {
		_ = json.Unmarshal(b, &t)
	}
	return t
}

// markdownToHTML renders a chapter's markdown to standalone HTML using the same
// parser extensions the corpus is built with. The result is inserted as-is into
// the page (it is already well-formed server-side HTML).
func markdownToHTML(md string) template.HTML {
	return template.HTML(markdown.ToHTML([]byte(md), nil, nil))
}

// escText returns an HTML-escaped string for any value type the templates pass
// to the esc function.
func escText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return html.EscapeString(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case []string:
		return html.EscapeString(strings.Join(t, ", "))
	default:
		return html.EscapeString(fmt.Sprint(v))
	}
}

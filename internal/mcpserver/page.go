package mcpserver

import (
	"embed"
	"fmt"
	"html"
	"html/template"
	"strconv"
	"strings"

	"github.com/gomarkdown/markdown"
)

// pages holds the per-tool page templates. They live here, next to the tools
// they render, while the global page chrome (header, nav, styles, index) stays
// in the command's own ui.html shell.
//
//go:embed page_documents.html
//go:embed page_read.html
//go:embed page_search.html
//go:embed page_command.html
//go:embed page_term.html
var pages embed.FS

// PageTemplates returns the file system holding the per-tool page templates,
// so the command can parse them into the same template set as its shell.
func PageTemplates() embed.FS { return pages }

// PageData is the shared data every web page is rendered with. Fields are the
// union of what the global chrome and the individual tool pages need; each tool
// fills in only its own block via its Build*PageData helper.
type PageData struct {
	// global chrome
	Page      string
	PageTitle string
	Summary   *CorpusOverview
	Products  []string
	Versions  []string

	// documents
	Product string
	Version string
	Docs    *ListDocumentsOut

	// read
	Read        *ReadDocumentOut
	ChapterHTML template.HTML

	// search
	Query  string
	Limit  int
	Search *SearchDocsOut

	// command
	Pattern string
	Command *FindCommandOut

	// term
	TermQ string
	Term  *DefineTermOut
}

// PageFuncs returns the template functions shared by the shell and the page
// templates. esc avoids double-escaping the already-trusted chapter HTML.
func PageFuncs() template.FuncMap {
	return template.FuncMap{
		"esc":  func(s any) template.HTML { return template.HTML(escText(s)) },
		"join": func(ss []string) string { return strings.Join(ss, ", ") },
		"str":  func(v any) string { return fmt.Sprint(v) },
	}
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

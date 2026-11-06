package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"docullm/internal/docparse"
	"docullm/internal/mcpserver"
	"github.com/gomarkdown/markdown"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

//go:embed ui.html
var ui embed.FS

// markdownToHTML renders a chapter's markdown to standalone HTML using the same
// parser extensions the corpus is built with. The result is inserted as-is into
// the page (it is already well-formed server-side HTML).
func markdownToHTML(md string) template.HTML {
	return template.HTML(markdown.ToHTML([]byte(md), nil, nil))
}

type pageData struct {
	Page    string // index | documents | read | search | command | term
	Title   string
	Summary *mcpserver.CorpusOverview

	// the distinct products and versions in the corpus, used to constrain the
	// distro filters (mirrors the MCP input-schema enum)
	Products []string
	Versions []string

	// documents
	Product string
	Version string
	Docs    *mcpserver.ListDocumentsOut

	// read
	Read *mcpserver.ReadDocumentOut
	// the rendered markdown of the selected chapter, if any
	ChapterHTML template.HTML

	// search
	Query   string
	Limit   int
	Search  *mcpserver.SearchDocsOut

	// command
	Pattern string
	Command *mcpserver.FindCommandOut

	// term
	TermQ string
	Term  *mcpserver.DefineTermOut
}

// webApp serves the server-rendered web interface and JSON API over corpus c.
type webApp struct {
	corpus *docparse.Corpus
	tmpl   *template.Template
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func errorJSON(w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}

// renderPage fills data and writes the page through the shared template.
func (a *webApp) renderPage(w http.ResponseWriter, r *http.Request, data pageData) {
	if data.Page == "" {
		data.Page = "index"
	}
	if data.Title == "" {
		data.Title = "docuLLM"
	}
	if data.Summary == nil {
		ov := mcpserver.CorpusSummary(a.corpus)
		data.Summary = &ov
	}
	data.Products = data.Summary.Products
	data.Versions = data.Summary.Versions
	if err := a.tmpl.ExecuteTemplate(w, "ui.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// serveWeb returns the http handler for the web interface and API over the corpus c.
func serveWeb(c *docparse.Corpus) http.Handler {
	// esc and join are shared template functions (esc avoids double-escaping the
	// chapter HTML which is already trusted server-side markup).
	tmpl, err := template.New("page").Funcs(template.FuncMap{
		"esc":  func(s any) template.HTML { return template.HTML(escText(s)) },
		"join": func(ss []string) string { return strings.Join(ss, ", ") },
		"str":  func(v any) string { return fmt.Sprint(v) },
	}).ParseFS(ui, "ui.html")
	if err != nil {
		panic(err)
	}
	app := &webApp{corpus: c, tmpl: tmpl}

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			errorJSON(w, http.StatusNotFound, "not found")
			return
		}
		app.renderPage(w, r, pageData{Page: "index", Title: "Documentation tools"})
	})

	// ---- HTML pages (pure server-rendered, no JavaScript) ----

	mux.HandleFunc("/documents", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		product, version := q.Get("product"), q.Get("version")
		docs := mcpserver.ListDocuments(app.corpus, product, version)
		app.renderPage(w, r, pageData{
			Page: "documents", Title: "Documents",
			Product: product, Version: version, Docs: &docs,
		})
	})

	mux.HandleFunc("/document/{docID}", func(w http.ResponseWriter, r *http.Request) {
		docID := r.PathValue("docID")
		out, err := mcpserver.BuildReadDocument(app.corpus, docID, "")
		if err != nil {
			app.renderPage(w, r, pageData{Page: "read", Title: "Read"})
			return
		}
		app.renderPage(w, r, pageData{
			Page: "read", Title: out.Title, Read: &out,
			ChapterHTML: "",
		})
	})

	mux.HandleFunc("/chapter/{docID}/{chapterID}", func(w http.ResponseWriter, r *http.Request) {
		docID, chID := r.PathValue("docID"), r.PathValue("chapterID")
		out, err := mcpserver.BuildReadDocument(app.corpus, docID, chID)
		if err != nil {
			app.renderPage(w, r, pageData{Page: "read", Title: "Read"})
			return
		}
		var chHTML template.HTML
		if out.Chapter != nil {
			chHTML = markdownToHTML(out.Chapter.Markdown)
		}
		app.renderPage(w, r, pageData{
			Page: "read", Title: out.Title, Read: &out, ChapterHTML: chHTML,
		})
	})

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		query := q.Get("query")
		limit, _ := strconv.Atoi(q.Get("limit"))
		var search *mcpserver.SearchDocsOut
		if query != "" {
			s := mcpserver.SearchDocs(app.corpus, query, q.Get("product"), q.Get("version"), limit)
			search = &s
		}
		app.renderPage(w, r, pageData{
			Page: "search", Title: "Search",
			Query: query, Product: q.Get("product"), Version: q.Get("version"), Limit: limit, Search: search,
		})
	})

	mux.HandleFunc("/command", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		pattern := q.Get("pattern")
		var command *mcpserver.FindCommandOut
		if pattern != "" {
			c := mcpserver.FindCommand(app.corpus, pattern)
			command = &c
		}
		app.renderPage(w, r, pageData{
			Page: "command", Title: "Find command",
			Pattern: pattern, Command: command,
		})
	})

	mux.HandleFunc("/term", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		term := q.Get("term")
		var def *mcpserver.DefineTermOut
		if term != "" {
			d := mcpserver.DefineTerm(app.corpus, term)
			def = &d
		}
		app.renderPage(w, r, pageData{
			Page: "term", Title: "Define term",
			TermQ: term, Term: def,
		})
	})

	// ---- JSON API (unchanged contract, used by MCP clients and curl) ----

	mux.HandleFunc("/api/list_documents", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		writeJSON(w, mcpserver.ListDocuments(app.corpus, q.Get("product"), q.Get("version")))
	})

	mux.HandleFunc("/api/read_document", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		docID := q.Get("doc_id")
		if docID == "" {
			errorJSON(w, http.StatusBadRequest, "doc_id is required")
			return
		}
		out, err := mcpserver.BuildReadDocument(app.corpus, docID, q.Get("chapter_id"))
		if err != nil {
			errorJSON(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, out)
	})

	mux.HandleFunc("/api/search_docs", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		query := q.Get("query")
		if query == "" {
			errorJSON(w, http.StatusBadRequest, "query is required")
			return
		}
		limit, _ := strconv.Atoi(q.Get("limit"))
		writeJSON(w, mcpserver.SearchDocs(app.corpus, query, q.Get("product"), q.Get("version"), limit))
	})

	mux.HandleFunc("/api/find_command", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		pattern := q.Get("pattern")
		if pattern == "" {
			errorJSON(w, http.StatusBadRequest, "pattern is required")
			return
		}
		writeJSON(w, mcpserver.FindCommand(app.corpus, pattern))
	})

	mux.HandleFunc("/api/define_term", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		term := q.Get("term")
		if term == "" {
			errorJSON(w, http.StatusBadRequest, "term is required")
			return
		}
		writeJSON(w, mcpserver.DefineTerm(app.corpus, term))
	})

	mux.HandleFunc("/api/corpus", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, mcpserver.CorpusSummary(app.corpus))
	})

	// Convenience: redirect query params to the UI for easy deep-linking.
	mux.HandleFunc("/open", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if doc := q.Get("doc"); doc != "" {
			if ch := q.Get("chapter"); ch != "" {
				http.Redirect(w, r, "/chapter/"+url.PathEscape(doc)+"/"+url.PathEscape(ch), http.StatusFound)
			} else {
				http.Redirect(w, r, "/document/"+url.PathEscape(doc), http.StatusFound)
			}
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
	})

	if debug {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			mux.ServeHTTP(rec, r)
			fmt.Fprintf(os.Stderr, "[debug] web: %s %s -> %d (%s)\n", r.Method, r.URL.RequestURI(), rec.status, time.Since(start).Round(time.Microsecond))
		})
	}
	return mux
}

// escText returns a string for any of the value types the template passes in.
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

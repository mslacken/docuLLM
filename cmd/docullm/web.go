package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"docullm/internal/docparse"
	"docullm/internal/mcpserver"
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

// webApp serves the server-rendered web interface and JSON API over corpus c.
// The global page chrome lives in the embedded ui.html shell; each tool's page
// markup and page-data builder lives in the mcpserver package, next to the tool
// it mirrors.
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

// renderPage executes the shell (global chrome) with the given PageData, which
// each tool's page-data builder already filled in.
func (a *webApp) renderPage(w http.ResponseWriter, data mcpserver.PageData) {
	if err := a.tmpl.ExecuteTemplate(w, "ui.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// newTemplate parses the global ui.html shell and the per-tool page templates
// (from the mcpserver package) into a single template set.
func newTemplate() (*template.Template, error) {
	tmpl, err := template.New("page").Funcs(mcpserver.PageFuncs()).ParseFS(ui, "ui.html")
	if err != nil {
		return nil, err
	}
	return tmpl.ParseFS(mcpserver.PageTemplates(),
		"page_documents.html", "page_read.html", "page_search.html",
		"page_command.html", "page_term.html")
}

// serveWeb returns the http handler for the web interface and API over the corpus c.
func serveWeb(c *docparse.Corpus) http.Handler {
	tmpl, err := newTemplate()
	if err != nil {
		panic(err)
	}
	app := &webApp{corpus: c, tmpl: tmpl}

	mux := http.NewServeMux()

	// ---- HTML pages (pure server-rendered, no JavaScript) ----

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			errorJSON(w, http.StatusNotFound, "not found")
			return
		}
		app.renderPage(w, mcpserver.BuildIndexPageData(app.corpus))
	})

	mux.HandleFunc("/documents", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		app.renderPage(w, mcpserver.BuildListDocumentsPageData(app.corpus, q.Get("product"), q.Get("version")))
	})

	mux.HandleFunc("/document/{docID}", func(w http.ResponseWriter, r *http.Request) {
		app.renderPage(w, mcpserver.BuildReadDocumentPageData(app.corpus, r.PathValue("docID"), ""))
	})

	mux.HandleFunc("/chapter/{docID}/{chapterID}", func(w http.ResponseWriter, r *http.Request) {
		app.renderPage(w, mcpserver.BuildReadDocumentPageData(app.corpus, r.PathValue("docID"), r.PathValue("chapterID")))
	})

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		app.renderPage(w, mcpserver.BuildSearchPageData(app.corpus, q.Get("query"), q.Get("product"), q.Get("version"), limit))
	})

	mux.HandleFunc("/command", func(w http.ResponseWriter, r *http.Request) {
		app.renderPage(w, mcpserver.BuildFindCommandPageData(app.corpus, r.URL.Query().Get("pattern")))
	})

	mux.HandleFunc("/term", func(w http.ResponseWriter, r *http.Request) {
		app.renderPage(w, mcpserver.BuildDefineTermPageData(app.corpus, r.URL.Query().Get("term")))
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

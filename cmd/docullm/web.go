package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"docullm/internal/docparse"
	"docullm/internal/mcpserver"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
// It is a plain MCP client: it talks to the same server the -mcp flag serves,
// over an in-memory transport, so every page shows exactly what an MCP client
// would get. The global page chrome lives in the embedded ui.html shell; each
// tool's page template lives in the mcpserver package, next to the tool, and
// calls the tool itself via the "tool" template function.
type webApp struct {
	session *mcp.ClientSession
	schemas map[string]inputSchema
	summary mcpserver.CorpusOverview
	tmpl    *template.Template
}

// inputSchema is the part of a tool's JSON input schema the web interface
// needs: the argument types (to convert form values) and their enums (to fill
// the form's select boxes).
type inputSchema struct {
	Properties map[string]struct {
		Type string   `json:"type"`
		Enum []string `json:"enum"`
	} `json:"properties"`
}

// toolResult is the outcome of a tool call as the templates see it: the
// decoded structured output, or the error text the tool returned.
type toolResult struct {
	Out   map[string]any
	Error string
	raw   json.RawMessage
}

// page is the data the ui.html shell is executed with. Page names the tool
// whose template renders the body ("index" for the overview), Args are that
// tool's arguments as given in the request, one entry per schema property.
type page struct {
	Page    string
	Title   string
	Args    map[string]string
	Summary mcpserver.CorpusOverview
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

// connect starts the MCP server for c and returns a client session connected
// to it over an in-memory transport.
func connect(ctx context.Context, c *docparse.Corpus) (*mcp.ClientSession, error) {
	clientT, serverT := mcp.NewInMemoryTransports()
	if _, err := mcpserver.NewServer(c).Connect(ctx, serverT, nil); err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "docullm-web", Version: "0.1.0"}, nil)
	return client.Connect(ctx, clientT, nil)
}

// newWebApp connects to the MCP server for c and fetches the tool schemas.
func newWebApp(c *docparse.Corpus) (*webApp, error) {
	ctx := context.Background()
	session, err := connect(ctx, c)
	if err != nil {
		return nil, err
	}
	app := &webApp{session: session, schemas: map[string]inputSchema{}, summary: mcpserver.CorpusSummary(c)}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	for _, t := range tools.Tools {
		b, err := json.Marshal(t.InputSchema)
		if err != nil {
			return nil, err
		}
		var s inputSchema
		if err := json.Unmarshal(b, &s); err != nil {
			return nil, err
		}
		app.schemas[t.Name] = s
	}
	if app.tmpl, err = app.newTemplate(); err != nil {
		return nil, err
	}
	return app, nil
}

// callTool calls the tool name with the given form values, converted to the
// types of the tool's input schema; empty values are left out, as an MCP
// client would omit them.
func (a *webApp) callTool(name string, args map[string]string) toolResult {
	schema, ok := a.schemas[name]
	if !ok {
		return toolResult{Error: fmt.Sprintf("unknown tool %q", name)}
	}
	in := map[string]any{}
	for k, v := range args {
		if v == "" {
			continue
		}
		if schema.Properties[k].Type == "integer" {
			if n, err := strconv.Atoi(v); err == nil {
				in[k] = n
				continue
			}
		}
		in[k] = v
	}
	res, err := a.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: in})
	if err != nil {
		return toolResult{Error: err.Error()}
	}
	if res.IsError {
		var msg []string
		for _, c := range res.Content {
			if t, ok := c.(*mcp.TextContent); ok {
				msg = append(msg, t.Text)
			}
		}
		return toolResult{Error: strings.Join(msg, "\n")}
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return toolResult{Error: err.Error()}
	}
	out := toolResult{raw: raw}
	// UseNumber keeps numbers as sent (no float formatting like 1.2e+06).
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&out.Out); err != nil {
		return toolResult{Error: err.Error()}
	}
	return out
}

// enum returns the allowed values of a tool argument, as advertised in the
// tool's input schema.
func (a *webApp) enum(tool, prop string) []string {
	return a.schemas[tool].Properties[prop].Enum
}

// args collects the arguments of tool from the request: path values (e.g.
// {doc_id}) first, then query parameters. Every schema property gets an entry,
// so templates can use .Args.<name> for the form values.
func (a *webApp) args(tool string, r *http.Request) map[string]string {
	out := map[string]string{}
	for prop := range a.schemas[tool].Properties {
		v := r.PathValue(prop)
		if v == "" {
			v = r.URL.Query().Get(prop)
		}
		out[prop] = v
	}
	return out
}

// newTemplate parses the global ui.html shell and the per-tool page templates
// (from the mcpserver package) into a single template set. Besides the shared
// page functions the templates get "tool", which calls an MCP tool, and
// "enum", which returns the allowed values of one of its arguments.
func (a *webApp) newTemplate() (*template.Template, error) {
	funcs := mcpserver.PageFuncs()
	funcs["tool"] = a.callTool
	funcs["enum"] = a.enum
	tmpl, err := template.New("page").Funcs(funcs).ParseFS(ui, "ui.html")
	if err != nil {
		return nil, err
	}
	files := make([]string, len(mcpserver.PageNames))
	for i, n := range mcpserver.PageNames {
		files[i] = n + ".html"
	}
	return tmpl.ParseFS(mcpserver.PageTemplates(), files...)
}

// renderPage executes the shell for p. The output is buffered, so a failing
// template yields a clean error instead of a half written page.
func (a *webApp) renderPage(w http.ResponseWriter, p page) {
	p.Summary = a.summary
	var b bytes.Buffer
	if err := a.tmpl.ExecuteTemplate(&b, "ui.html", p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	b.WriteTo(w)
}

// toolPage serves the page of tool, called with the request's arguments.
func (a *webApp) toolPage(tool, title string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.renderPage(w, page{Page: tool, Title: title, Args: a.args(tool, r)})
	}
}

// serveWeb returns the http handler for the web interface and API over the corpus c.
func serveWeb(c *docparse.Corpus) http.Handler {
	app, err := newWebApp(c)
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()

	// ---- HTML pages (pure server-rendered, no JavaScript) ----

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			errorJSON(w, http.StatusNotFound, "not found")
			return
		}
		app.renderPage(w, page{Page: "index", Title: "Documentation tools"})
	})
	mux.HandleFunc("/documents", app.toolPage("list_documents", "Documents"))
	mux.HandleFunc("/document/{doc_id}", app.toolPage("read_document", "Read document"))
	mux.HandleFunc("/chapter/{doc_id}/{chapter_id}", app.toolPage("read_document", "Read document"))
	mux.HandleFunc("/search", app.toolPage("search_docs", "Search"))
	mux.HandleFunc("/command", app.toolPage("find_command", "Find command"))
	mux.HandleFunc("/term", app.toolPage("define_term", "Define term"))

	// ---- JSON API: the tool's structured output, as an MCP client gets it ----

	mux.HandleFunc("/api/{tool}", func(w http.ResponseWriter, r *http.Request) {
		tool := r.PathValue("tool")
		if _, ok := app.schemas[tool]; !ok {
			errorJSON(w, http.StatusNotFound, "unknown tool")
			return
		}
		res := app.callTool(tool, app.args(tool, r))
		if res.Error != "" {
			errorJSON(w, http.StatusBadRequest, res.Error)
			return
		}
		writeJSON(w, res.raw)
	})

	mux.HandleFunc("/api/corpus", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, app.summary)
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

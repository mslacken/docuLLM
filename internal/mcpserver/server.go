package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"docullm/internal/docparse"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var Debug bool

func debugf(format string, args ...any) {
	if Debug {
		fmt.Fprintf(os.Stderr, "[debug] tool: "+format+"\n", args...)
	}
}

type listDocumentsIn struct {
	Product string `json:"product,omitempty" jsonschema:"only documents whose product contains this text, e.g. 'Register Product'"`
	Version string `json:"version,omitempty" jsonschema:"only documents for this product version, e.g. '16.0'"`
}

type documentInfo struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Distro   docparse.Distro `json:"distro"`
	Chapters int             `json:"chapters"`
}

type ListDocumentsOut struct {
	Documents []documentInfo `json:"documents"`
}

type readDocumentIn struct {
	DocID     string `json:"doc_id" jsonschema:"document id as returned by list_documents or search_docs"`
	ChapterID string `json:"chapter_id,omitempty" jsonschema:"chapter id from the table of contents; if omitted only the table of contents is returned"`
}

type tocEntry struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Summary  string `json:"summary,omitempty"`
	Commands int    `json:"commands"`
}

type chapterOut struct {
	ID         string                   `json:"id"`
	Title      string                   `json:"title"`
	SourcePath string                   `json:"source_path"`
	Start      uint                     `json:"start"`
	End        uint                     `json:"end"`
	Markdown   string                   `json:"markdown"`
	Commands   []string                 `json:"commands,omitempty"`
	Glossary   []docparse.GlossaryEntry `json:"glossary,omitempty"`
}

type ReadDocumentOut struct {
	documentInfo
	TOC     []tocEntry  `json:"toc"`
	Chapter *chapterOut `json:"chapter,omitempty"`
}

type searchDocsIn struct {
	Query   string `json:"query" jsonschema:"search words"`
	Product string `json:"product,omitempty" jsonschema:"only documents whose product contains this text"`
	Version string `json:"version,omitempty" jsonschema:"only documents for this product version"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum number of hits, default 10"`
}

type SearchDocsOut struct {
	Hits []docparse.SearchHit `json:"hits"`
}

type findCommandIn struct {
	Pattern string `json:"pattern" jsonschema:"words the command must contain, e.g. 'crm configure primitive'"`
}

type FindCommandOut struct {
	Commands []docparse.CommandHit `json:"commands"`
}

type defineTermIn struct {
	Term string `json:"term" jsonschema:"glossary term or abbreviation, e.g. 'CIB' or 'split brain'"`
}

type DefineTermOut struct {
	Entries []docparse.GlossaryEntry `json:"entries"`
}

func info(d *docparse.Document) documentInfo {
	return documentInfo{ID: d.ID, Title: d.Title, Distro: d.Distro, Chapters: len(d.Chapters)}
}

func BuildReadDocument(c *docparse.Corpus, docID, chapterID string) (ReadDocumentOut, error) {
	doc := c.Document(docID)
	if doc == nil {
		return ReadDocumentOut{}, fmt.Errorf("unknown document %q, use list_documents to get the ids", docID)
	}
	out := ReadDocumentOut{documentInfo: info(doc), TOC: []tocEntry{}}
	for _, ch := range doc.Chapters {
		out.TOC = append(out.TOC, tocEntry{ID: ch.ID, Title: ch.Title, Summary: ch.Summary, Commands: len(ch.Commands)})
	}
	if chapterID == "" {
		return out, nil
	}
	ch := doc.Chapter(chapterID)
	if ch == nil {
		return ReadDocumentOut{}, fmt.Errorf("unknown chapter %q in document %q", chapterID, docID)
	}
	src := ch.SourcePath
	if src == "" {
		src = ch.FilePath
	}
	out.Chapter = &chapterOut{
		ID: ch.ID, Title: ch.Title, SourcePath: src, Start: ch.Start, End: ch.End,
		Markdown: c.Text(ch), Commands: nonNil(ch.Commands), Glossary: c.GlossaryFor(ch.Terms),
	}
	return out, nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// ListDocuments returns the documents matching product and version; an empty
// selector matches everything.
func ListDocuments(c *docparse.Corpus, product, version string) ListDocumentsOut {
	out := ListDocumentsOut{Documents: []documentInfo{}}
	for i := range c.Documents {
		if c.Documents[i].Matches(product, version) {
			out.Documents = append(out.Documents, info(&c.Documents[i]))
		}
	}
	return out
}

// SearchDocs ranks chapters for query, optionally filtered by product/version.
func SearchDocs(c *docparse.Corpus, query, product, version string, limit int) SearchDocsOut {
	if limit <= 0 {
		limit = 10
	}
	return SearchDocsOut{Hits: nonNil(c.Search(query, product, version, limit))}
}

// FindCommand returns the documented commands containing every word of pattern.
func FindCommand(c *docparse.Corpus, pattern string) FindCommandOut {
	return FindCommandOut{Commands: nonNil(c.FindCommand(pattern))}
}

// DefineTerm looks up term in the glossary, falling back to partial matches.
func DefineTerm(c *docparse.Corpus, term string) DefineTermOut {
	return DefineTermOut{Entries: nonNil(c.DefineTerm(term))}
}

// corpusSummary is the shape of the corpus overview endpoint.
type CorpusOverview struct {
	Documents int      `json:"documents"`
	Chapters  int      `json:"chapters"`
	Glossary  int      `json:"glossary"`
	Products  []string `json:"products"`
	Versions  []string `json:"versions"`
	Terms     int      `json:"terms"`
	Commands  int      `json:"commands"`
}

// CorpusSummary returns aggregate counts and the distinct product/version
// values, plus the number of glossary terms and documented commands.
func CorpusSummary(c *docparse.Corpus) CorpusOverview {
	s := CorpusOverview{
		Documents: len(c.Documents),
		Glossary:  len(c.Glossary),
		Terms:     len(c.Glossary),
	}
	products := map[string]bool{}
	versions := map[string]bool{}
	for i := range c.Documents {
		d := &c.Documents[i]
		if d.Distro.Product != "" {
			products[d.Distro.Product] = true
		}
		for _, v := range d.Distro.Versions {
			versions[v] = true
		}
		for j := range d.Chapters {
			s.Chapters++
			s.Commands += len(d.Chapters[j].Commands)
		}
	}
	s.Products = sortedKeys(products)
	s.Versions = sortedKeys(versions)
	return s
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// setEnum restricts a property to the given values. An empty list leaves the
// property unrestricted. The enum is derived from the corpus, so it only ever
// contains products and versions that actually occur in the documents.
func setEnum(s *jsonschema.Schema, prop string, values []string) {
	if p := s.Properties[prop]; p != nil && len(values) > 0 {
		p.Enum = make([]any, len(values))
		for i, v := range values {
			p.Enum[i] = v
		}
	}
}

// CreateListDocumentsSchema returns the input schema for list_documents with
// product and version restricted to the values present in the corpus.
func CreateListDocumentsSchema(c *docparse.Corpus) *jsonschema.Schema {
	schema, _ := jsonschema.For[listDocumentsIn](nil)
	if schema != nil {
		ov := CorpusSummary(c)
		setEnum(schema, "product", ov.Products)
		setEnum(schema, "version", ov.Versions)
	}
	return schema
}

// CreateSearchDocsSchema returns the input schema for search_docs with product
// and version restricted to the values present in the corpus.
func CreateSearchDocsSchema(c *docparse.Corpus) *jsonschema.Schema {
	schema, _ := jsonschema.For[searchDocsIn](nil)
	if schema != nil {
		ov := CorpusSummary(c)
		setEnum(schema, "product", ov.Products)
		setEnum(schema, "version", ov.Versions)
	}
	return schema
}

// NewServer builds the MCP server exposing the corpus' documents.
func NewServer(c *docparse.Corpus) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "docullm", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: "Product documentation. Use search_docs or list_documents to find a document, " +
			"read_document to get its table of contents or a chapter, find_command to find where a " +
			"command is documented and define_term for the glossary.",
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_documents",
		Description: "List the available documents (guides) with id, title, summary and product versions.",
		InputSchema: CreateListDocumentsSchema(c),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in listDocumentsIn) (*mcp.CallToolResult, ListDocumentsOut, error) {
		debugf("list_documents product=%q version=%q", in.Product, in.Version)
		return nil, ListDocuments(c, in.Product, in.Version), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "read_document",
		Description: "Return a document's metadata and table of contents. If chapter_id is given, also return " +
			"that chapter's markdown, the commands it shows and the definitions of all glossary terms it mentions.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in readDocumentIn) (*mcp.CallToolResult, ReadDocumentOut, error) {
		debugf("read_document doc_id=%q chapter_id=%q", in.DocID, in.ChapterID)
		out, err := BuildReadDocument(c, in.DocID, in.ChapterID)
		debugf("read_document result: %d toc entries, chapter=%v err=%v", len(out.TOC), out.Chapter != nil, err)
		return nil, out, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_docs",
		Description: "Full text search over all chapters. Returns ranked hits with doc_id and chapter_id for read_document.",
		InputSchema: CreateSearchDocsSchema(c),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in searchDocsIn) (*mcp.CallToolResult, SearchDocsOut, error) {
		debugf("search_docs query=%q product=%q version=%q limit=%d", in.Query, in.Product, in.Version, in.Limit)
		out := SearchDocs(c, in.Query, in.Product, in.Version, in.Limit)
		debugf("search_docs result: %d hits", len(out.Hits))
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "find_command",
		Description: "Find the documented shell commands containing all given words and the chapters showing them.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in findCommandIn) (*mcp.CallToolResult, FindCommandOut, error) {
		debugf("find_command pattern=%q", in.Pattern)
		out := FindCommand(c, in.Pattern)
		debugf("find_command result: %d commands", len(out.Commands))
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "define_term",
		Description: "Look up a term or abbreviation in the glossary.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in defineTermIn) (*mcp.CallToolResult, DefineTermOut, error) {
		debugf("define_term term=%q", in.Term)
		out := DefineTerm(c, in.Term)
		debugf("define_term result: %d entries", len(out.Entries))
		return nil, out, nil
	})

	addResources(srv, c)
	return srv
}

func addResources(srv *mcp.Server, c *docparse.Corpus) {
	jsonResult := func(uri string, v any) (*mcp.ReadResourceResult, error) {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(data)}}}, nil
	}

	srv.AddResource(&mcp.Resource{
		URI: "docs://index", Name: "index", MIMEType: "application/json",
		Description: "All documents with id, title, summary and product versions",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		docs := []documentInfo{}
		for i := range c.Documents {
			docs = append(docs, info(&c.Documents[i]))
		}
		return jsonResult(req.Params.URI, docs)
	})

	srv.AddResource(&mcp.Resource{
		URI: "docs://glossary", Name: "glossary", MIMEType: "application/json",
		Description: "The glossary shared by all documents",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return jsonResult(req.Params.URI, c.Glossary)
	})

	// docs://{doc_id} and docs://{doc_id}/{chapter_id}, same content as read_document
	handler := func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		docID, chapterID, _ := strings.Cut(strings.TrimPrefix(uri, "docs://"), "/")
		out, err := BuildReadDocument(c, docID, chapterID)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		if out.Chapter != nil {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: out.Chapter.Markdown}}}, nil
		}
		return jsonResult(uri, out)
	}
	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "docs://{doc_id}", Name: "document", MIMEType: "application/json",
		Description: "Metadata and table of contents of a document",
	}, handler)
	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "docs://{doc_id}/{chapter_id}", Name: "chapter", MIMEType: "text/markdown",
		Description: "Markdown of a chapter",
	}, handler)
}

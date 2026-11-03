package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listDocumentsIn struct {
	Product string `json:"product,omitempty" jsonschema:"only documents whose product contains this text, e.g. 'High Availability'"`
	Version string `json:"version,omitempty" jsonschema:"only documents for this product version, e.g. '16.0'"`
}

type documentInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Distro   Distro `json:"distro"`
	License  string `json:"license,omitempty"`
	Chapters int    `json:"chapters"`
}

type listDocumentsOut struct {
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
	ID         string          `json:"id"`
	Title      string          `json:"title"`
	SourcePath string          `json:"source_path"`
	Start      uint            `json:"start"`
	End        uint            `json:"end"`
	Markdown   string          `json:"markdown"`
	Commands   []string        `json:"commands"`
	Glossary   []GlossaryEntry `json:"glossary"`
}

type readDocumentOut struct {
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

type searchDocsOut struct {
	Hits []searchHit `json:"hits"`
}

type findCommandIn struct {
	Pattern string `json:"pattern" jsonschema:"words the command must contain, e.g. 'crm configure primitive'"`
}

type findCommandOut struct {
	Commands []commandHit `json:"commands"`
}

type defineTermIn struct {
	Term string `json:"term" jsonschema:"glossary term or abbreviation, e.g. 'CIB' or 'split brain'"`
}

type defineTermOut struct {
	Entries []GlossaryEntry `json:"entries"`
}

func info(d *Document) documentInfo {
	return documentInfo{ID: d.ID, Title: d.Title, Summary: d.Summary, Distro: d.Distro, License: d.License, Chapters: len(d.Chapters)}
}

func (c *Corpus) readDocument(docID, chapterID string) (readDocumentOut, error) {
	doc := c.document(docID)
	if doc == nil {
		return readDocumentOut{}, fmt.Errorf("unknown document %q, use list_documents to get the ids", docID)
	}
	out := readDocumentOut{documentInfo: info(doc), TOC: []tocEntry{}}
	for _, ch := range doc.Chapters {
		out.TOC = append(out.TOC, tocEntry{ID: ch.ID, Title: ch.Title, Summary: ch.Summary, Commands: len(ch.Commands)})
	}
	if chapterID == "" {
		return out, nil
	}
	ch := doc.chapter(chapterID)
	if ch == nil {
		return readDocumentOut{}, fmt.Errorf("unknown chapter %q in document %q", chapterID, docID)
	}
	src := ch.SourcePath
	if src == "" {
		src = ch.FilePath
	}
	out.Chapter = &chapterOut{
		ID: ch.ID, Title: ch.Title, SourcePath: src, Start: ch.Start, End: ch.End,
		Markdown: c.text(ch), Commands: nonNil(ch.Commands), Glossary: c.glossaryFor(ch.Terms),
	}
	return out, nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func newMCPServer(c *Corpus) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "docullm", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: "Product documentation. Use search_docs or list_documents to find a document, " +
			"read_document to get its table of contents or a chapter, find_command to find where a " +
			"command is documented and define_term for the glossary.",
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_documents",
		Description: "List the available documents (guides) with id, title, summary and product versions.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in listDocumentsIn) (*mcp.CallToolResult, listDocumentsOut, error) {
		out := listDocumentsOut{Documents: []documentInfo{}}
		for i := range c.Documents {
			if c.Documents[i].matches(in.Product, in.Version) {
				out.Documents = append(out.Documents, info(&c.Documents[i]))
			}
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "read_document",
		Description: "Return a document's metadata and table of contents. If chapter_id is given, also return " +
			"that chapter's markdown, the commands it shows and the definitions of all glossary terms it mentions.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in readDocumentIn) (*mcp.CallToolResult, readDocumentOut, error) {
		out, err := c.readDocument(in.DocID, in.ChapterID)
		return nil, out, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_docs",
		Description: "Full text search over all chapters. Returns ranked hits with doc_id and chapter_id for read_document.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in searchDocsIn) (*mcp.CallToolResult, searchDocsOut, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 10
		}
		return nil, searchDocsOut{Hits: nonNil(c.search(in.Query, in.Product, in.Version, limit))}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "find_command",
		Description: "Find the documented shell commands containing all given words and the chapters showing them.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in findCommandIn) (*mcp.CallToolResult, findCommandOut, error) {
		return nil, findCommandOut{Commands: nonNil(c.findCommand(in.Pattern))}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "define_term",
		Description: "Look up a term or abbreviation in the glossary.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in defineTermIn) (*mcp.CallToolResult, defineTermOut, error) {
		return nil, defineTermOut{Entries: nonNil(c.defineTerm(in.Term))}, nil
	})

	addResources(srv, c)
	return srv
}

func addResources(srv *mcp.Server, c *Corpus) {
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
		out, err := c.readDocument(docID, chapterID)
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

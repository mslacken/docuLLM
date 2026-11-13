package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"docullm/internal/docparse"
	"docullm/internal/docparse/markdown"
)

// sampleCorpus is a small corpus with one document (two chapters, a command)
// and a glossary, enough to exercise every web page.
const sampleCorpus = `# Combined Documentation

---

<!-- Source: guide/index.md -->

---
title: Guide
description: A sample guide
product: Sample Product
version: "1.0"
---

# Guide

## Setup

The CIB holds the configuration of every node.

` + "```" + `
# crm configure show
` + "```" + `

## Quorum

The quorum prevents split brain.

---

<!-- Source: guide/ha-glossary.md -->

# Glossary

CIB (cluster information base)

The cluster configuration.

DC (designated coordinator)

The coordinator.
`

func buildCorpus() *docparse.Corpus {
	return markdown.BuildDocuments(markdown.SplitSources([]byte(sampleCorpus), "combined.md"))
}

// TestWebPagesRender requests every page through the web handler, so the
// global ui.html shell, each tool's page template and the in-memory MCP tool
// call behind it are exercised together, and asserts the expected output.
func TestWebPagesRender(t *testing.T) {
	h := serveWeb(buildCorpus())

	cases := []struct {
		name    string
		url     string
		want    []string
		notWant []string
	}{
		{name: "index", url: "/", want: []string{"/documents", "/search", "/command", "/term"}},
		{name: "documents", url: "/documents", want: []string{"Guide", "Sample Product", "Read &rarr;", `<option value="" selected>All</option>`}},
		{name: "documents-filter", url: "/documents?product=Nope", want: []string{"validating", `<option value="">All</option>`}},
		{name: "documents-version", url: "/documents?version=1.0", want: []string{`<option value="">All</option>`, `<option value="1.0" selected>`}},
		{name: "read-toc", url: "/document/guide", want: []string{"Guide", "Table of contents", "Setup", "Quorum", "/chapter/guide/setup"}},
		{name: "read-chapter", url: "/chapter/guide/setup",
			want:    []string{"crm configure show", "CIB", "Commands in this chapter"},
			notWant: []string{"Table of contents", "/chapter/guide/quorum"}},
		{name: "read-unknown", url: "/document/nope", want: []string{"unknown document", "Browse the documents"}},
		{name: "search", url: "/search?query=quorum&limit=10", want: []string{"quorum", "open &rarr;"}},
		{name: "search-docs", url: "/search?query=cib&docs=guide", want: []string{"Setup", `name="docs" value="guide" checked`}},
		{name: "search-docs-empty", url: "/search?query=cib&docs=zzz", want: []string{"No results"}},
		{name: "search-regex", url: "/search?query=bra&regex=on", want: []string{"Quorum", "open &rarr;", `name="regex" value="on" checked`}},
		{name: "search-scope-title", url: "/search?query=setup&scope=title", want: []string{"Setup", `value="title" selected`}},
		{name: "search-scope-summary", url: "/search?query=split&scope=summary", want: []string{"Quorum", "open &rarr;", `value="summary" selected`}},
		{name: "search-scope-empty", url: "/search?query=zzz&scope=title", want: []string{"No results"}},
		{name: "search-empty", url: "/search", want: []string{"Search documents"}},
		{name: "command", url: "/command?pattern=crm+configure", want: []string{"crm configure show"}},
		{name: "command-docs", url: "/command?pattern=crm+configure&docs=guide", want: []string{"crm configure show", `name="docs" value="guide" checked`}},
		{name: "command-docs-empty", url: "/command?pattern=crm+configure&docs=zzz", want: []string{"No matching commands"}},
		{name: "command-empty", url: "/command?pattern=zzz", want: []string{"No matching commands"}},
		{name: "term", url: "/term?term=CIB", want: []string{"cluster information base", "The cluster configuration."}},
		{name: "term-empty", url: "/term?term=zzz", want: []string{"No glossary entry"}},
	}

	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", tc.url, nil))
		out := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d: %s", tc.name, rec.Code, out)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: output missing %q\n---\n%s", tc.name, w, out)
			}
		}
		for _, w := range tc.notWant {
			if strings.Contains(out, w) {
				t.Errorf("%s: output unexpectedly contains %q\n---\n%s", tc.name, w, out)
			}
		}
		if strings.Contains(out, "no value") {
			t.Errorf("%s: output contains a missing template value", tc.name)
		}
		if !strings.Contains(out, "<!doctype html>") || !strings.Contains(out, "<header>") {
			t.Errorf("%s: missing global chrome (doctype/header)", tc.name)
		}
	}
}

// TestWebAPI checks the JSON API returns the tools' structured output and
// reports tool errors (like a missing required argument) as bad requests.
func TestWebAPI(t *testing.T) {
	h := serveWeb(buildCorpus())

	cases := []struct {
		url    string
		status int
		want   string
	}{
		{"/api/list_documents", http.StatusOK, `"id": "guide"`},
		{"/api/read_document?doc_id=guide&chapter_id=setup", http.StatusOK, `"markdown"`},
		{"/api/read_document", http.StatusBadRequest, "doc_id"},
		{"/api/search_docs?query=quorum&limit=1", http.StatusOK, `"hits"`},
		{"/api/find_command?pattern=crm", http.StatusOK, "crm configure show"},
		{"/api/define_term?term=CIB", http.StatusOK, "cluster information base"},
		{"/api/corpus", http.StatusOK, `"documents": 1`},
		{"/api/nope", http.StatusNotFound, "unknown tool"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", tc.url, nil))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: got %d %q, want %d containing %q", tc.url, rec.Code, rec.Body.String(), tc.status, tc.want)
		}
	}
}

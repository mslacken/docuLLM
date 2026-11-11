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
		name string
		url  string
		want []string
	}{
		{"index", "/", []string{"/documents", "/search", "/command", "/term"}},
		{"documents", "/documents", []string{"Guide", "Sample Product", "Read &rarr;", `<option value="" selected>All</option>`}},
		{"documents-filter", "/documents?product=Nope", []string{"validating", `<option value="">All</option>`}},
		{"documents-version", "/documents?version=1.0", []string{`<option value="">All</option>`, `<option value="1.0" selected>`}},
		{"read-toc", "/document/guide", []string{"Guide", "Table of contents", "Setup", "Quorum", "/chapter/guide/setup"}},
		{"read-chapter", "/chapter/guide/setup", []string{"crm configure show", "CIB", "Commands in this chapter"}},
		{"read-unknown", "/document/nope", []string{"unknown document", "Browse the documents"}},
		{"search", "/search?query=quorum&limit=10", []string{"quorum", "open &rarr;"}},
		{"search-docs", "/search?query=cib&docs=guide", []string{"Setup", `name="docs" value="guide" checked`}},
		{"search-docs-empty", "/search?query=cib&docs=zzz", []string{"No results"}},
		{"search-regex", "/search?query=bra&regex=on", []string{"Quorum", "open &rarr;", `name="regex" value="on" checked`}},
		{"search-empty", "/search", []string{"Search documents"}},
		{"command", "/command?pattern=crm+configure", []string{"crm configure show"}},
		{"command-docs", "/command?pattern=crm+configure&docs=guide", []string{"crm configure show", `name="docs" value="guide" checked`}},
		{"command-docs-empty", "/command?pattern=crm+configure&docs=zzz", []string{"No matching commands"}},
		{"command-empty", "/command?pattern=zzz", []string{"No matching commands"}},
		{"term", "/term?term=CIB", []string{"cluster information base", "The cluster configuration."}},
		{"term-empty", "/term?term=zzz", []string{"No glossary entry"}},
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

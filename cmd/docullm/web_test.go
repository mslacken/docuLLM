package main

import (
	"strings"
	"testing"

	"docullm/internal/docparse"
	"docullm/internal/mcpserver"
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
	return docparse.BuildDocuments(docparse.SplitSources([]byte(sampleCorpus), "combined.md"))
}

// TestWebPagesRender executes the global ui.html shell together with each tool's
// page template and builder, asserting the page renders without error and
// contains the expected output. This is the integration the refactor depends on.
func TestWebPagesRender(t *testing.T) {
	tmpl, err := newTemplate()
	if err != nil {
		t.Fatalf("newTemplate: %v", err)
	}
	c := buildCorpus()

	cases := []struct {
		name string
		data mcpserver.PageData
		want []string
	}{
		{"index", mcpserver.BuildIndexPageData(c), []string{"/documents", "/search", "/command", "/term"}},
		{"documents", mcpserver.BuildListDocumentsPageData(c, "", ""), []string{"Guide", "Sample Product", "Read &rarr;", `<option value="" selected>All</option>`}},
		{"documents-filter", mcpserver.BuildListDocumentsPageData(c, "Nope", ""), []string{"No documents match", `<option value="">All</option>`}},
		{"documents-version", mcpserver.BuildListDocumentsPageData(c, "", "1.0"), []string{`<option value="">All</option>`}},
		{"read-toc", mcpserver.BuildReadDocumentPageData(c, "guide", ""), []string{"Guide", "Table of contents", "Setup", "Quorum"}},
		{"read-chapter", mcpserver.BuildReadDocumentPageData(c, "guide", "setup"), []string{"crm configure show", "CIB", "Commands in this chapter"}},
		{"read-unknown", mcpserver.BuildReadDocumentPageData(c, "nope", ""), []string{"Unknown document"}},
		{"search", mcpserver.BuildSearchPageData(c, "quorum", "", "", 10), []string{"quorum", "open &rarr;"}},
		{"search-empty", mcpserver.BuildSearchPageData(c, "", "", "", 10), []string{"Search documents"}},
		{"command", mcpserver.BuildFindCommandPageData(c, "crm configure"), []string{"crm configure show"}},
		{"command-empty", mcpserver.BuildFindCommandPageData(c, "zzz"), []string{"No matching commands"}},
		{"term", mcpserver.BuildDefineTermPageData(c, "CIB"), []string{"cluster information base", "The cluster configuration."}},
		{"term-empty", mcpserver.BuildDefineTermPageData(c, "zzz"), []string{"No glossary entry"}},
	}

	for _, tc := range cases {
		var b strings.Builder
		if err := tmpl.ExecuteTemplate(&b, "ui.html", tc.data); err != nil {
			t.Errorf("%s: render error: %v", tc.name, err)
			continue
		}
		out := b.String()
		for _, w := range tc.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: output missing %q\n---\n%s", tc.name, w, out)
			}
		}
		if !strings.Contains(out, "<!doctype html>") || !strings.Contains(out, "<header>") {
			t.Errorf("%s: missing global chrome (doctype/header)", tc.name)
		}
	}
}

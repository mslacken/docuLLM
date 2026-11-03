package mcpserver

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"docullm/internal/docparse"
)

const sample = `# Combined Documentation

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

## Setup

Second chapter with the same title.

---

<!-- Source: guide/ha-glossary.md -->

# Glossary

CIB (cluster information base)

The cluster configuration.

node

A cluster member.

DC (designated coordinator)

The coordinator.
`

func TestReadDocument(t *testing.T) {
	c := docparse.BuildDocuments(docparse.SplitSources([]byte(sample), "combined.md"))
	if len(c.Documents) != 1 || len(c.Glossary) != 3 {
		t.Fatalf("got %d documents and %d glossary entries", len(c.Documents), len(c.Glossary))
	}
	out, err := BuildReadDocument(c, "guide", "setup")
	if err != nil {
		t.Fatal(err)
	}
	if ids := []string{out.TOC[0].ID, out.TOC[1].ID}; !reflect.DeepEqual(ids, []string{"setup", "setup-2"}) {
		t.Errorf("toc ids = %q", ids)
	}
	ch := out.Chapter
	if !strings.HasPrefix(ch.Markdown, "## Setup\n\nThe CIB") || ch.SourcePath != "guide/index.md" {
		t.Errorf("chapter = %+v", ch)
	}
	var terms []string
	for _, g := range ch.Glossary {
		terms = append(terms, g.Term)
	}
	sort.Strings(terms)
	if want := []string{"CIB (cluster information base)", "node"}; !reflect.DeepEqual(terms, want) {
		t.Errorf("glossary terms = %q, want %q", terms, want)
	}
	if !reflect.DeepEqual(ch.Commands, []string{"crm configure show"}) {
		t.Errorf("commands = %q", ch.Commands)
	}
	if _, err := BuildReadDocument(c, "guide", "nope"); err == nil {
		t.Error("expected error for unknown chapter")
	}
}

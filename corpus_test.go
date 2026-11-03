package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestAliases(t *testing.T) {
	for term, want := range map[string][]string{
		"CIB (cluster information base)": {"CIB", "cluster information base"},
		"active/active, active/passive":  {"active/active", "active/passive"},
		"Pacemaker":                      {"Pacemaker"},
	} {
		if got := aliases(term); !reflect.DeepEqual(got, want) {
			t.Errorf("aliases(%q) = %q, want %q", term, got, want)
		}
	}
}

func TestGlossaryMatchers(t *testing.T) {
	for _, tc := range []struct {
		term, text string
		want       bool
	}{
		{"CIB (cluster information base)", "the CIB is replicated", true},
		{"CIB (cluster information base)", "the Cluster Information Base", true},
		{"CIB (cluster information base)", "cib", false},
		{"DC (designated coordinator)", "cd /etc/dc", false},
		{"ReaR (Relax and Recover)", "the rear node", false},
		{"split brain", "avoid a split-brain scenario", true},
		{"clone", "two clones of it", true},
		{"node", "nodes", true},
		{"node", "nodejs", false},
		{"location constraint", "add a location   constraint", true},
	} {
		got := false
		for _, re := range glossaryMatchers(tc.term) {
			got = got || re.MatchString(tc.text)
		}
		if got != tc.want {
			t.Errorf("%q in %q = %v, want %v", tc.term, tc.text, got, tc.want)
		}
	}
}

func TestProseSkipsCode(t *testing.T) {
	prose := proseOf(parseNodes(strings.Split("## Title\n\nUse `crm` here.\n\n```\n# crm configure\n```\n", "\n")))
	if strings.Contains(prose, "crm") || !strings.Contains(prose, "Use  here.") {
		t.Errorf("prose = %q", prose)
	}
}

func TestUniqueID(t *testing.T) {
	seen := map[string]int{}
	got := []string{uniqueID(seen, slug("Overview")), uniqueID(seen, slug("Overview")), uniqueID(seen, slug("1.2 What's new?"))}
	want := []string{"overview", "overview-2", "1-2-what-s-new"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

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
	c := buildDocuments(splitSources([]byte(sample), "combined.md"))
	if len(c.Documents) != 1 || len(c.Glossary) != 3 {
		t.Fatalf("got %d documents and %d glossary entries", len(c.Documents), len(c.Glossary))
	}
	out, err := c.readDocument("guide", "setup")
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
	if want := []string{"CIB (cluster information base)", "node"}; !reflect.DeepEqual(terms, want) {
		t.Errorf("glossary terms = %q, want %q", terms, want)
	}
	if !reflect.DeepEqual(ch.Commands, []string{"crm configure show"}) {
		t.Errorf("commands = %q", ch.Commands)
	}
	if _, err := c.readDocument("guide", "nope"); err == nil {
		t.Error("expected error for unknown chapter")
	}
}

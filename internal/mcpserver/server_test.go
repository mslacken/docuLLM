package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"docullm/internal/docparse/loader"
	"docullm/internal/docparse/markdown"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	c := markdown.BuildDocuments(markdown.SplitSources([]byte(sample), "combined.md"))
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

// docbookFiles is a minimal DocBook assembly tree with all the structured
// information the DocBook parser records only for the CLI dump.
var docbookFiles = map[string]string{
	"DC-SLES-demo": "MAIN=\"demo.asm.xml\"\nSRC_DIR=\"articles\"\nPROFOS=\"sles\"\n",
	"common/entities.ent": `<!ENTITY prompt.sudo "<prompt xmlns='http://docbook.org/ns/docbook'>&gt; </prompt><command xmlns='http://docbook.org/ns/docbook'>sudo</command> ">
<!ENTITY productname "<phrase xmlns='http://docbook.org/ns/docbook'><phrase os='sles'>Demo Server</phrase><phrase os='other'>Other</phrase></phrase>">`,
	"articles/demo.asm.xml": `<?xml version="1.0"?>
<!DOCTYPE assembly [ <!ENTITY % e SYSTEM "../common/entities.ent"> %e; ]>
<assembly xmlns="http://docbook.org/ns/docbook">
  <resources>
    <resource href="../tasks/task.xml" xml:id="_task"/>
    <resource href="../references/glossary.xml" xml:id="_glossary"/>
  </resources>
  <structure xml:id="demo">
    <merge>
      <title>Demo</title><subtitle>Sub</subtitle>
      <revhistory><revision><date>2026-01-01</date><revdescription><para>Initial</para></revdescription></revision></revhistory>
      <meta name="maintainer" content="tux@example.com"/>
      <meta name="productname"><productname version="1.0">&productname;</productname></meta>
      <dm:docmanager xmlns:dm="urn:x-suse:ns:docmanager"><dm:bugtracker><dm:url>https://bugs</dm:url></dm:bugtracker><dm:translation>yes</dm:translation></dm:docmanager>
    </merge>
    <module resourceref="_task"/>
    <module resourceref="_glossary" renderas="glossary"/>
  </structure>
</assembly>`,
	"tasks/task.xml": `<?xml version="1.0"?>
<!DOCTYPE topic [ <!ENTITY % e SYSTEM "../common/entities.ent"> %e; ]>
<!-- refers to legacy doc: https://github.com/SUSE/doc-sle/blob/main/xml/x.xml -->
<topic xml:id="task" role="task" xmlns="http://docbook.org/ns/docbook" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:xi="http://www.w3.org/2001/XInclude">
  <info><title>Install</title><meta name="maintainer" content="geeko@example.com"/></info>
  <para>The CIB, see <xref linkend="sec"/> and <link xlink:href="https://example.com">docs</link>.</para>
  <procedure><step><para>Run:</para><screen>&prompt.sudo;zypper in demo</screen></step></procedure>
  <note><para>Careful.</para></note>
  <xi:include href="../snippets/tip.xml"/>
  <mediaobject><imageobject><imagedata fileref="a.png"/></imageobject></mediaobject>
  <section xml:id="sec"><title>Section</title><para>Text.</para></section>
</topic>`,
	"snippets/tip.xml": `<tip xmlns="http://docbook.org/ns/docbook"><para>Included tip.</para></tip>`,
	"references/glossary.xml": `<?xml version="1.0"?>
<topic xml:id="glossary" xmlns="http://docbook.org/ns/docbook">
  <glossentry><glossterm>CIB (cluster information base)</glossterm><glossdef><para>The configuration.</para></glossdef></glossentry>
</topic>`,
}

// TestDocBookFieldsNotExposed checks that the structured DocBook information
// shows up in the CLI dump but in none of the MCP tool or resource results.
func TestDocBookFieldsNotExposed(t *testing.T) {
	root := t.TempDir()
	for name, content := range docbookFiles {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := loader.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	hidden := []string{"source", "profile", "subtitle", "meta", "revisions", "bugtracker", "translation",
		"topic_type", "render_as", "maintainer", "legacy_sources", "sections", "procedures", "admonitions",
		"xrefs", "links", "images", "includes"}
	dump, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range hidden {
		if !strings.Contains(string(dump), `"`+k+`":`) {
			t.Errorf("CLI dump lacks %q", k)
		}
	}

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := NewServer(c).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	var outputs []string
	for name, args := range map[string]map[string]any{
		"list_documents": {"product": "Demo Server", "version": "1.0"},
		"read_document":  {"doc_id": "sles-demo", "chapter_id": "task"},
		"search_docs":    {"query": "install"},
		"find_command":   {"pattern": "zypper"},
		"define_term":    {"term": "CIB"},
	} {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %+v", name, err, res)
		}
		data, _ := json.Marshal(res)
		outputs = append(outputs, string(data))
	}
	for _, uri := range []string{"docs://index", "docs://glossary", "docs://sles-demo", "docs://sles-demo/task"} {
		res, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("%s: %v", uri, err)
		}
		data, _ := json.Marshal(res)
		outputs = append(outputs, string(data))
	}
	all := strings.Join(outputs, "\n")
	for _, k := range hidden {
		if strings.Contains(all, `"`+k+`":`) || strings.Contains(all, `\"`+k+`\":`) {
			t.Errorf("MCP output exposes %q", k)
		}
	}
	for _, want := range []string{"sudo zypper in demo", "Demo Server", "The configuration.", "## Install {#task}"} {
		if !strings.Contains(all, want) {
			t.Errorf("MCP output lacks %q", want)
		}
	}
}

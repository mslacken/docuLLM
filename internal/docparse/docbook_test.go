package docparse

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeFiles creates the files (name -> content) below a new temp dir.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const doctype = `<!DOCTYPE topic [
  <!ENTITY % entities SYSTEM "../common/generic-entities.ent">
  %entities;
]>
`

var entityFiles = map[string]string{
	"common/generic-entities.ent": `<!-- <!ENTITY commented "no"> -->
<!ENTITY % product-entities SYSTEM "product-entities.ent">
%product-entities;
<!ENTITY suse "SUSE">
<!ENTITY sles "&suse; Linux Enterprise Server">
<!ENTITY sles4sap '&sles; for SAP applications'>
<!ENTITY prompt.sudo "<prompt xmlns='http://docbook.org/ns/docbook'>&gt; </prompt><command xmlns='http://docbook.org/ns/docbook'>sudo</command> ">
<!ENTITY prompt.root "<prompt role='root' xmlns='http://docbook.org/ns/docbook'># </prompt>">
<!ENTITY loop "a &loop; b">
<!ENTITY nbsp "&#x000A0;">
`,
	"common/product-entities.ent": `<!ENTITY productname "<phrase xmlns='http://docbook.org/ns/docbook'><phrase os='sles'>&sles;</phrase><phrase os='sles4sap'>&sles4sap;</phrase></phrase>">
<!ENTITY productnumber "16.1">
<!ENTITY productnumber "17.0">
`,
}

func TestEntityExpansion(t *testing.T) {
	root := writeFiles(t, entityFiles)
	l := newEntityLoader()
	subset := `<!ENTITY sles "Local Server"> <!ENTITY % entities SYSTEM "../common/generic-entities.ent"> %entities;`
	tab := l.table(filepath.Join(root, "articles"), subset)
	for in, want := range map[string]string{
		"&productnumber;":            "16.1",
		"&sles;":                     "Local Server", // first declaration wins
		"&sles4sap;":                 "Local Server for SAP applications",
		"&suse;":                     "SUSE",
		"&prompt.sudo;zypper":        "<prompt xmlns='http://docbook.org/ns/docbook'>&gt; </prompt><command xmlns='http://docbook.org/ns/docbook'>sudo</command> zypper",
		"a &lt; b &amp; &#x41;":      "a &lt; b &amp; &#x41;",
		"&undefined;":                "&amp;undefined;",
		"&commented;":                "&amp;commented;",
		"<!-- &suse; -->&suse;":      "<!-- &suse; -->SUSE",
		"<![CDATA[&suse;]]>":         "<![CDATA[&suse;]]>",
		"&loop;":                     "a &amp;loop; b",
		"x&nbsp;y":                   "x&#x000A0;y",
		"<para os='a'>&suse;</para>": "<para os='a'>SUSE</para>",
	} {
		if got := tab.expand(in); got != want {
			t.Errorf("expand(%q) = %q, want %q", in, got, want)
		}
	}
	if got := tab.expand("&productname;"); !strings.Contains(got, "<phrase os='sles'>Local Server</phrase>") {
		t.Errorf("productname = %q", got)
	}
	if l.table(filepath.Join(root, "articles"), subset) != tab {
		t.Error("table not cached")
	}
}

func TestSplitDoctype(t *testing.T) {
	doc := `<?xml version="1.0"?>
<!-- comment ] > -->
<!DOCTYPE topic [
  <!-- a ] in a comment -->
  <!ENTITY x "va]l>ue">
]>
<topic/>`
	body, subset := splitDoctype(doc)
	if strings.Contains(body, "DOCTYPE") || !strings.Contains(body, "<topic/>") || !strings.Contains(body, "<!-- comment ] > -->") {
		t.Errorf("body = %q", body)
	}
	if !strings.Contains(subset, `<!ENTITY x "va]l>ue">`) {
		t.Errorf("subset = %q", subset)
	}
}

func TestProfile(t *testing.T) {
	root, err := decodeXML(`<topic><para os="sles;sles4sap">a</para><para os="sleha">b</para>` +
		`<para arch="x86-64">c</para><para arch="zseries">d</para><para condition="beta">e</para><para>f</para></topic>`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		p    profile
		want string
	}{
		{profile{os: profileValues("sles"), arch: profileValues("x86_64")}, "acef"},
		{profile{os: profileValues("sleha"), condition: profileValues("beta;suse-product")}, "bcdef"},
		{profile{}, "abcdef"},
	} {
		if got := tc.p.apply(root).flatText(); strings.ReplaceAll(got, " ", "") != tc.want {
			t.Errorf("%+v: got %q, want %q", tc.p, got, tc.want)
		}
	}
	if !reflect.DeepEqual(profileValues(" X86-64; arm ;;power"), []string{"x86_64", "aarch64", "power"}) {
		t.Errorf("profileValues = %q", profileValues(" X86-64; arm ;;power"))
	}
}

// docbookFixture is a small doc-modular like tree: two DC files publishing
// the same assembly for different products, one DC file with a missing
// assembly, a task with a procedure, an xincluded snippet, a container module
// and a glossary.
func docbookFixture(t *testing.T) string {
	files := map[string]string{
		"DC-SLES-demo": "# comment\nMAIN=\"demo.asm.xml\"\nSRC_DIR=\"articles\"\nIMG_SRC_DIR=\"images\"\nPROFOS=\"sles\"\nPROFCONDITION=\"suse-product;16.0\"\nSTRUCTID=\"demo-b\"\n#PROFOS=\"sleha\"\n",
		"DC-SAP-demo":  "MAIN='demo.asm.xml'\nSRC_DIR=articles\nPROFOS=\"sles4sap\"\nSTRUCTID=\"demo-b\"\n",
		"DC-SLES-gone": "MAIN=\"missing.asm.xml\"\nSRC_DIR=\"articles\"\n",
		"_DC-disabled": "MAIN=\"demo.asm.xml\"\nSRC_DIR=\"articles\"\n",
		"README.md":    "# Not a document\n",
		"articles/demo.asm.xml": `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE assembly [
  <!ENTITY % entities SYSTEM "../common/generic-entities.ent">
  %entities;
]>
<assembly version="5.2" xmlns="http://docbook.org/ns/docbook" xmlns:xlink="http://www.w3.org/1999/xlink">
  <resources>
    <resource href="../tasks/install.xml" xml:id="_install"/>
    <resource href="../concepts/about.xml" xml:id="_about"/>
    <resource href="../references/glossary.xml" xml:id="_glossary"/>
    <resource href="../common/legal.xml" xml:id="_legal"/>
    <resource href="../tasks/does-not-exist.xml" xml:id="_unused"/>
    <!-- <resource href="../tasks/commented.xml" xml:id="_install"/> -->
  </resources>
  <structure xml:id="demo-a" renderas="article">
    <merge><title>Wrong structure</title></merge>
    <module resourceref="_about"/>
  </structure>
  <structure xml:id="demo-b" renderas="article">
    <merge>
      <title>Demo on &productname;</title>
      <subtitle>A subtitle</subtitle>
      <revhistory><revision><date>2026-01-02</date><revdescription><para>Initial
        version</para></revdescription></revision></revhistory>
      <meta name="maintainer" content="tux@example.com"/>
      <meta name="series">Products &amp; Solutions</meta>
      <meta name="task"><phrase>Installation</phrase><phrase>Administration</phrase></meta>
      <meta name="architecture"><phrase os="sles">x86</phrase><phrase os="sles4sap">ppc</phrase></meta>
      <meta name="productname"><productname version="&productnumber;" os="sles;sles4sap">&productname;</productname></meta>
      <meta name="description">Meta description</meta>
      <meta name="someone@example.com">malformed</meta>
      <meta name="updated" content="2026-02-03"/>
      <dm:docmanager xmlns:dm="urn:x-suse:ns:docmanager">
        <dm:bugtracker><dm:url>https://bugzilla.example.com</dm:url><dm:component>Docs</dm:component>
          <dm:product>Demo 16.1</dm:product><dm:assignee>tux@example.com</dm:assignee></dm:bugtracker>
        <dm:translation>yes</dm:translation>
      </dm:docmanager>
      <abstract><para>Lead of the <emphasis>demo</emphasis>.</para><para><emphasis role="bold">Prerequisites</emphasis></para><itemizedlist><listitem><para>none</para></listitem></itemizedlist></abstract>
    </merge>
    <module resourceref="_install" renderas="section">
      <merge><title>Install it</title></merge>
    </module>
    <module xml:id="more" renderas="section">
      <merge><title>More</title><abstract><para>Container abstract.</para></abstract></merge>
      <module resourceref="_about"/>
    </module>
    <module resourceref="_legal"/>
    <module resourceref="_glossary" renderas="glossary"/>
  </structure>
</assembly>
`,
		"tasks/install.xml": `<?xml version="1.0" encoding="UTF-8"?>
` + doctype + `<!-- refers to legacy doc: https://github.com/SUSE/doc-sle/blob/main/xml/install.xml -->
<topic xml:id="install" role="task" xmlns="http://docbook.org/ns/docbook" xmlns:xi="http://www.w3.org/2001/XInclude" xmlns:xlink="http://www.w3.org/1999/xlink">
  <info>
    <title>Installing</title>
    <meta name="maintainer" content="geeko@example.com"/>
    <abstract><para>How to install the <command>demo</command>.</para></abstract>
  </info>
  <para os="sles">Only on SLES.</para>
  <para os="sles4sap">Only on SAP.</para>
  <para>See <xref linkend="about-sec"/>, <xref linkend="dangling"/> and <link xlink:href="https://example.com/docs">the docs</link>.<remark>editorial</remark></para>
  <procedure xml:id="proc-install">
    <title>Installing the package</title>
    <step><para>Install the package:</para>
      <screen>&prompt.sudo;<command>zypper in <replaceable>PKG</replaceable></command><co xml:id="co-1"/></screen>
      <substeps><step><para>Check it.</para></step></substeps>
    </step>
    <xi:include href="../snippets/reboot-step.xml"/>
  </procedure>
  <note><title>Be careful</title><para>A note with <guimenu>Menu</guimenu>.</para></note>
  <variablelist><varlistentry><term><option>--force</option></term><listitem><para>Force it.</para></listitem></varlistentry></variablelist>
  <figure><title>Overview</title><mediaobject><imageobject><imagedata fileref="overview.png"/></imageobject></mediaobject></figure>
  <table><title>Options</title><tgroup cols="2"><thead><row><entry>Name</entry><entry>Meaning</entry></row></thead>
    <tbody><row><entry><literal>a|b</literal></entry><entry><para>One</para><para>Two</para></entry></row></tbody></tgroup></table>
  <section xml:id="install-verify"><title>Verifying</title>
    <screen language="bash">&prompt.root;rpm -q demo</screen>
  </section>
</topic>
`,
		"snippets/reboot-step.xml": `<!-- include like this: <xi:include href="../snippets/reboot-step.xml"/> -->
<!DOCTYPE step [
  <!ENTITY % entities SYSTEM "../common/generic-entities.ent">
  %entities;
]>
<step xmlns="http://docbook.org/ns/docbook"><para>Reboot:</para><screen>&prompt.sudo;reboot now</screen></step>
`,
		"concepts/about.xml": `<?xml version="1.0" encoding="UTF-8"?>
` + doctype + `<topic xml:id="about" role="concept" xmlns="http://docbook.org/ns/docbook">
  <title>About the demo</title>
  <info><abstract><para/></abstract></info>
  <para>The CIB of &productname;.</para>
  <section xml:id="about-sec"><title>Details</title><para>Details here.</para></section>
</topic>
`,
		"references/glossary.xml": `<?xml version="1.0" encoding="UTF-8"?>
` + doctype + `<topic xml:id="glossary" role="reference" xmlns="http://docbook.org/ns/docbook">
  <info><title>Glossary</title></info>
  <glossdiv><title>General</title>
  <glossentry xml:id="gloss-cib"><glossterm>CIB (cluster information base)</glossterm>
    <glossdef><para>The cluster configuration, see <xref linkend="gloss-node"/>.</para></glossdef></glossentry>
  </glossdiv>
  <glossentry xml:id="gloss-node"><glossterm>node</glossterm><glossdef><para>A <emphasis>member</emphasis>.</para></glossdef></glossentry>
</topic>
`,
		"common/legal.xml": `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE topic [
  <!ENTITY % entities SYSTEM "generic-entities.ent">
  %entities;
]>
<topic xml:id="legal-disclaimer" xmlns="http://docbook.org/ns/docbook">
  <info><title>Legal Notice</title></info>
  <para>Permission is granted to copy, distribute and/or modify this document under the terms of the
    GNU Free Documentation License, Version 1.2 or (at your option) version 1.3; with the Invariant
    Section being this copyright notice and license.</para>
</topic>
`,
	}
	for k, v := range entityFiles {
		files[k] = v
	}
	return writeFiles(t, files)
}

func TestDocBookCorpus(t *testing.T) {
	c, err := LoadCorpus(docbookFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, d := range c.Documents {
		ids = append(ids, d.ID)
	}
	if want := []string{"sap-demo", "sles-demo"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("documents = %q, want %q", ids, want)
	}

	sap, sles := c.Document("sap-demo"), c.Document("sles-demo")
	if sles.Title != "Demo on SUSE Linux Enterprise Server" || sap.Title != "Demo on SUSE Linux Enterprise Server for SAP applications" {
		t.Errorf("titles = %q, %q", sles.Title, sap.Title)
	}
	if sles.Summary != "Lead of the demo." {
		t.Errorf("summary = %q", sles.Summary)
	}
	if want := (Distro{Name: "SUSE Linux Enterprise Server", Versions: []string{"16.1", "16.0"}}); !reflect.DeepEqual(sles.Distro, want) {
		t.Errorf("distro = %+v", sles.Distro)
	}
	if sles.License != "GFDL-1.2-invariants-only OR GFDL-1.3-invariants-only" {
		t.Errorf("license = %q", sles.License)
	}
	if want := (DocSource{DC: "DC-SLES-demo", Assembly: "articles/demo.asm.xml", Structure: "demo-b"}); *sles.Source != want {
		t.Errorf("source = %+v", *sles.Source)
	}
	wantMeta := DocMeta{
		Maintainer: "tux@example.com", Series: "Products & Solutions", Tasks: []string{"Installation", "Administration"},
		Architectures: []string{"x86"}, Updated: "2026-02-03", Description: "Meta description",
	}
	if !reflect.DeepEqual(*sles.Meta, wantMeta) || sap.Meta.Architectures[0] != "ppc" {
		t.Errorf("meta = %+v", *sles.Meta)
	}
	if sles.Subtitle != "A subtitle" || sles.Translation != "yes" || sles.Bugtracker.Component != "Docs" ||
		!reflect.DeepEqual(sles.Revisions, []Revision{{Date: "2026-01-02", Description: "Initial version"}}) {
		t.Errorf("subtitle %q, translation %q, bugtracker %+v, revisions %+v", sles.Subtitle, sles.Translation, sles.Bugtracker, sles.Revisions)
	}

	// legal notice and glossary are not chapters
	if len(sles.Chapters) != 2 {
		t.Fatalf("chapters = %+v", sles.Chapters)
	}
	ch := sles.Chapters[0]
	if ch.ID != "install" || ch.Title != "Install it" || ch.FilePath != "tasks/install.xml" || ch.TopicType != "task" ||
		ch.Maintainer != "geeko@example.com" || ch.Summary != "How to install the demo." {
		t.Errorf("chapter = %+v", ch)
	}
	if want := []string{"sudo zypper in PKG", "sudo reboot now", "rpm -q demo"}; !reflect.DeepEqual(ch.Commands, want) {
		t.Errorf("commands = %q, want %q", ch.Commands, want)
	}
	if !reflect.DeepEqual(ch.Includes, []string{"snippets/reboot-step.xml"}) ||
		!reflect.DeepEqual(ch.LegacySources, []string{"https://github.com/SUSE/doc-sle/blob/main/xml/install.xml"}) {
		t.Errorf("includes %q, legacy %q", ch.Includes, ch.LegacySources)
	}
	if want := []Xref{{"about-sec", true}, {"dangling", false}}; !reflect.DeepEqual(ch.Xrefs, want) {
		t.Errorf("xrefs = %+v", ch.Xrefs)
	}
	if !reflect.DeepEqual(ch.Links, []Link{{URL: "https://example.com/docs", Text: "the docs"}}) ||
		!reflect.DeepEqual(ch.Images, []Image{{File: "images/overview.png", Alt: "Overview"}}) ||
		!reflect.DeepEqual(ch.Procedures, []Procedure{{ID: "proc-install", Title: "Installing the package", Steps: 2}}) ||
		!reflect.DeepEqual(ch.Admonitions, []Admonition{{Type: "note", Title: "Be careful"}}) ||
		!reflect.DeepEqual(ch.Sections, []Section{{ID: "install-verify", Title: "Verifying", Level: 1}}) {
		t.Errorf("structure = %+v %+v %+v %+v %+v", ch.Links, ch.Images, ch.Procedures, ch.Admonitions, ch.Sections)
	}

	md := c.Text(&ch)
	for _, want := range []string{
		"## Install it {#install}\n\nHow to install the `demo`.\n\nOnly on SLES.\n\nSee [Details](#about-sec), [dangling](#dangling) and [the docs](https://example.com/docs).\n",
		"**Installing the package**\n\n1. Install the package:\n\n    ```console\n    > sudo zypper in PKG\n    ```\n\n    1. Check it.\n\n2. Reboot:\n",
		"> **Note:** Be careful\n>\n> A note with **Menu**.\n",
		"- **`--force`**\n\n    Force it.\n",
		"**Figure: Overview**\n\n![Overview](images/overview.png)\n",
		"**Table: Options**\n\n| Name | Meaning |\n| --- | --- |\n| `a\\|b` | One<br>Two |\n",
		"### Verifying {#install-verify}\n\n```bash\n# rpm -q demo\n```",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Only on SAP") || strings.Contains(md, "editorial") {
		t.Errorf("profiled or editorial text in markdown:\n%s", md)
	}
	if !strings.Contains(c.Text(&sap.Chapters[0]), "Only on SAP.") {
		t.Error("SAP variant lacks its profiled paragraph")
	}

	more := sles.Chapters[1]
	if more.ID != "more" || more.TopicType != "container" || more.Summary != "Container abstract." ||
		!reflect.DeepEqual(more.Sections, []Section{{ID: "about", Title: "About the demo", Level: 1, File: "concepts/about.xml"}, {ID: "about-sec", Title: "Details", Level: 2}}) {
		t.Errorf("container chapter = %+v", more)
	}
	if want := "## More {#more}\n\nContainer abstract.\n\n### About the demo {#about}\n\nThe CIB of SUSE Linux Enterprise Server.\n\n#### Details {#about-sec}"; !strings.HasPrefix(c.Text(&more), want) {
		t.Errorf("container markdown = %q", c.Text(&more))
	}
	if !reflect.DeepEqual(more.Terms, []string{"CIB (cluster information base)"}) {
		t.Errorf("terms = %q", more.Terms)
	}

	// shared glossary, deduplicated across the two documents
	want := []GlossaryEntry{
		{Term: "CIB (cluster information base)", WhatIs: "The cluster configuration, see node."},
		{Term: "node", WhatIs: "A member."},
	}
	if len(c.Glossary) != 2 || c.Glossary[0].Term != want[0].Term || c.Glossary[0].WhatIs != want[0].WhatIs || c.Glossary[1].WhatIs != want[1].WhatIs {
		t.Errorf("glossary = %+v", c.Glossary)
	}
	if hits := c.FindCommand("zypper"); len(hits) != 2 {
		t.Errorf("find zypper = %+v", hits)
	}
	if hits := c.Search("reboot", "for SAP", "", 0); len(hits) != 1 || hits[0].DocID != "sap-demo" {
		t.Errorf("search = %+v", hits)
	}
}

func TestSingleDCFile(t *testing.T) {
	root := docbookFixture(t)
	c, err := LoadCorpus(filepath.Join(root, "DC-SLES-demo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Documents) != 1 || c.Documents[0].ID != "sles-demo" {
		t.Errorf("documents = %+v", c.Documents)
	}
	if _, err := LoadCorpus(filepath.Join(root, "DC-SLES-gone")); err == nil {
		t.Error("expected error for a DC file without assembly")
	}
}

func TestStructureFallback(t *testing.T) {
	asm, err := decodeXML(`<assembly><structure xml:id="a"/><structure xml:id="b"/></assembly>`)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"": "a", "b": "b", "nope": "a"} {
		if got := selectStructure(asm, id).attr["id"]; got != want {
			t.Errorf("selectStructure(%q) = %q, want %q", id, got, want)
		}
	}
}

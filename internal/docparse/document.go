package docparse

import (
	"path"
	"regexp"
	"strings"

	"github.com/gomarkdown/markdown/parser"
	"gopkg.in/yaml.v3"
)

// Document is a parsed guide.
type Document struct {
	ID       string    `json:"id" yaml:"id"`
	Title    string    `json:"title" yaml:"title"`
	Summary  string    `json:"summary" yaml:"summary"`
	Distro   Distro    `json:"distro" yaml:"distro"`
	Product  Product   `json:"product" yaml:"product"`
	Chapters []Chapter `json:"chapters" yaml:"chapters"`
	License  string    `json:"license,omitempty" yaml:"license,omitempty"`

	// DocBook assemblies only; not exposed via MCP
	Source      *DocSource  `json:"source,omitempty" yaml:"source,omitempty"`
	Profile     *Profile    `json:"profile,omitempty" yaml:"profile,omitempty"`
	Subtitle    string      `json:"subtitle,omitempty" yaml:"subtitle,omitempty"`
	Meta        *DocMeta    `json:"meta,omitempty" yaml:"meta,omitempty"`
	Revisions   []Revision  `json:"revisions,omitempty" yaml:"revisions,omitempty"`
	Bugtracker  *Bugtracker `json:"bugtracker,omitempty" yaml:"bugtracker,omitempty"`
	Translation string      `json:"translation,omitempty" yaml:"translation,omitempty"` // "yes" or "no"
}

// Distro is the distribution a document applies to, e.g. "SUSE Linux
// Enterprise Server" and its versions (16.0, 16.1, ...).
type Distro struct {
	Name     string   `json:"name,omitempty" yaml:"name,omitempty"`
	Versions []string `json:"versions,omitempty" yaml:"versions,omitempty"`
}

// Product is a specific product (e.g. "SLES for SAP Applications") a document
// applies to. It is distinct from the Distro: both can carry a version.
type Product struct {
	Name     string   `json:"name,omitempty" yaml:"name,omitempty"`
	Versions []string `json:"versions,omitempty" yaml:"versions,omitempty"`
}

// Chapter is a top level section of a document.
type Chapter struct {
	ID         string   `json:"id" yaml:"id"`
	Title      string   `json:"chapter_title" yaml:"chapter_title"`
	Summary    string   `json:"summary,omitempty" yaml:"summary,omitempty"`         // lead paragraphs
	Start      uint     `json:"start" yaml:"start"`                                 // first line, counted in the original source file
	End        uint     `json:"end" yaml:"end"`                                     // last line, counted in the original source file
	FilePath   string   `json:"file_path" yaml:"file_path"`                         // file the chapter was parsed from (preferred)
	SourcePath string   `json:"source_path,omitempty" yaml:"source_path,omitempty"` // original file before combining
	Commands   []string `json:"commands,omitempty" yaml:"commands,omitempty"`
	Terms      []string `json:"terms,omitempty" yaml:"terms,omitempty"` // glossary terms mentioned in the prose

	// DocBook assemblies only; not exposed via MCP
	TopicType     string       `json:"topic_type,omitempty" yaml:"topic_type,omitempty"` // task, concept, reference, glue or container
	RenderAs      string       `json:"render_as,omitempty" yaml:"render_as,omitempty"`   // section, appendix, ...
	Maintainer    string       `json:"maintainer,omitempty" yaml:"maintainer,omitempty"`
	LegacySources []string     `json:"legacy_sources,omitempty" yaml:"legacy_sources,omitempty"` // "refers to" URLs of the original documentation
	Sections      []Section    `json:"sections,omitempty" yaml:"sections,omitempty"`
	Procedures    []Procedure  `json:"procedures,omitempty" yaml:"procedures,omitempty"`
	Admonitions   []Admonition `json:"admonitions,omitempty" yaml:"admonitions,omitempty"`
	Xrefs         []Xref       `json:"xrefs,omitempty" yaml:"xrefs,omitempty"`
	Links         []Link       `json:"links,omitempty" yaml:"links,omitempty"`
	Images        []Image      `json:"images,omitempty" yaml:"images,omitempty"`
	Includes      []string     `json:"includes,omitempty" yaml:"includes,omitempty"` // XIncluded snippet files

	source string // key into Corpus.lines
	prose  string // text outside of code blocks, used for glossary matching and search
}

// GlossaryEntry is a term and its definition.
type GlossaryEntry struct {
	Term   string `json:"term" yaml:"term"`
	WhatIs string `json:"whatis" yaml:"whatis"`

	match   []*regexp.Regexp // one per alias, see glossaryMatchers
	needles []string         // one per alias, see glossaryNeedles
}

// segment is one source file. For a combined file (see en-us/combine.sh) every
// `<!-- Source: ... -->` marker starts a new segment; lines[0] is line 1 of the
// original file.
type segment struct {
	file  string // original source file (from `<!-- Source: ... -->` or the input name)
	name  string // file the content was parsed from (the combined file, or input name if not combined)
	meta  map[string]any
	lines []string
	body  int // index of the first line after the front matter
}

var sourceMarker = regexp.MustCompile(`^<!-- Source: (.+?) -->\s*$`)

// SplitSources splits the markdown in data into one segment per <!-- Source: -->
// marker. For non combined input it returns a single segment named name.
func SplitSources(data []byte, name string) []segment {
	lines := strings.Split(string(data), "\n")
	var segs []segment
	file, start, combined := name, 0, false
	for i, ln := range lines {
		m := sourceMarker.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		// the preamble before the first marker is just the combine header
		if combined {
			segs = append(segs, newSegment(file, name, lines[start:i]))
		}
		combined = true
		file = m[1]
		// combine.sh writes a blank line after the marker
		start = min(i+2, len(lines))
	}
	return append(segs, newSegment(file, name, lines[start:]))
}

func newSegment(file, name string, lines []string) segment {
	// drop the "\n---\n\n" separator combine.sh puts before the next marker
	for len(lines) > 0 {
		t := strings.TrimSpace(lines[len(lines)-1])
		if t != "" && t != "---" {
			break
		}
		lines = lines[:len(lines)-1]
	}
	s := segment{file: file, name: name, lines: lines}
	if len(lines) > 0 && lines[0] == "---" {
		for j := 1; j < len(lines); j++ {
			if lines[j] == "---" {
				if yaml.Unmarshal([]byte(strings.Join(lines[1:j], "\n")), &s.meta) == nil {
					s.body = j + 1
				}
				break
			}
		}
	}
	return s
}

type heading struct {
	level int
	text  string
	idx   int // index into segment.lines
}

var (
	reATX   = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	reFence = regexp.MustCompile("^\\s*(```|~~~)")
)

func headings(s segment) []heading {
	var hs []heading
	fenced := false
	for i := s.body; i < len(s.lines); i++ {
		if reFence.MatchString(s.lines[i]) {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if m := reATX.FindStringSubmatch(s.lines[i]); m != nil {
			hs = append(hs, heading{level: len(m[1]), text: m[2], idx: i})
		}
	}
	return hs
}

var (
	reLicenseTitle = regexp.MustCompile(`(?i)^GNU Free Documentation License`)
	reHistory      = regexp.MustCompile(`(?i)^revision history`)
	reLegal        = regexp.MustCompile(`(?i)legal notice`)
	reChapterNum   = regexp.MustCompile(`^\d+(?:\.\d+)*\s*`)
	reTitleSuffix  = regexp.MustCompile(`\s*\|.*$`)
	reAppliesTo    = regexp.MustCompile(`^(?:Applies to )?(.+?) (\d+(?:\.\d+)*(?: SP\d+)?)$`)
)

func metaString(meta map[string]any, key string) string {
	if v, ok := meta[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return strings.Join(strings.Fields(s), " ")
		}
	}
	return ""
}

// BuildDocuments groups segments by directory (one directory per guide) and
// turns each group into a Document. The glossaries of all guides are merged
// into the corpus wide glossary.
func BuildDocuments(segs []segment) *Corpus {
	var order []string
	groups := map[string][]segment{}
	for _, s := range segs {
		dir := path.Dir(s.file)
		if _, ok := groups[dir]; !ok {
			order = append(order, dir)
		}
		groups[dir] = append(groups[dir], s)
	}
	c := &Corpus{Documents: []Document{}, lines: map[string][]string{}}
	for _, s := range segs {
		c.lines[s.file] = s.lines
	}
	ids := map[string]int{}
	for _, dir := range order {
		doc, glossary := buildDocument(groups[dir])
		doc.ID = uniqueID(ids, documentID(groups[dir][0].file))
		c.Documents = append(c.Documents, doc)
		c.addGlossary(glossary)
	}
	c.finish()
	return c
}

func buildDocument(segs []segment) (Document, []GlossaryEntry) {
	doc := Document{Chapters: []Chapter{}}
	var glossary []GlossaryEntry
	var legal, licenseFile []string
	var description string
	for _, s := range segs {
		addDistro(&doc.Distro, s)
		hs := headings(s)
		first := ""
		if len(hs) > 0 {
			first = hs[0].text
		}
		base := strings.ToLower(path.Base(s.file))
		switch {
		case strings.Contains(base, "license") || reLicenseTitle.MatchString(first):
			licenseFile = append(licenseFile, s.lines[s.body:]...)
		case strings.Contains(base, "glossary"):
			glossary = append(glossary, parseGlossary(s.lines[s.body:])...)
		case strings.HasPrefix(base, "rh-") || reHistory.MatchString(first):
		default:
			if doc.Title == "" {
				description = metaString(s.meta, "description")
				doc.Title = reTitleSuffix.ReplaceAllString(metaString(s.meta, "title"), "")
				for _, h := range hs {
					if h.level == 1 {
						doc.Title = h.text
						break
					}
				}
			}
			l, g := addChapters(&doc, s, hs)
			legal = append(legal, l...)
			glossary = append(glossary, g...)
		}
	}
	doc.Summary = description
	if doc.Summary == "" && len(doc.Chapters) > 0 {
		doc.Summary = doc.Chapters[0].Summary
	}
	doc.License = spdx(strings.Join(legal, "\n"))
	if doc.License == "" {
		doc.License = spdxFromLicenseText(strings.Join(licenseFile, "\n"))
	}
	ids := map[string]int{}
	for i := range doc.Chapters {
		doc.Chapters[i].ID = uniqueID(ids, slug(doc.Chapters[i].Title))
	}
	return doc, glossary
}

func addDistro(d *Distro, s segment) {
	product, version := metaString(s.meta, "product"), metaString(s.meta, "version")
	if product == "" || version == "" {
		// fall back to the "Applies to PRODUCT VERSION" line below the front matter
		for i := s.body; i < len(s.lines) && i < s.body+4; i++ {
			if m := reAppliesTo.FindStringSubmatch(strings.TrimSpace(s.lines[i])); m != nil && strings.HasPrefix(s.lines[i], "Applies to") {
				product, version = m[1], m[2]
				break
			}
		}
	}
	if d.Name == "" {
		d.Name = product
	}
	if version == "" {
		return
	}
	for _, v := range d.Versions {
		if v == version {
			return
		}
	}
	d.Versions = append(d.Versions, version)
}

// addChapters appends one Chapter per top level section of s and returns the
// text of the legal notice section(s) and the glossary sections, which are not
// treated as chapters.
func addChapters(doc *Document, s segment, hs []heading) ([]string, []GlossaryEntry) {
	level, h1 := 2, 0
	for _, h := range hs {
		if h.level == 1 {
			h1++
		}
	}
	if h1 > 1 {
		level = 1
	}
	var legal []string
	var glossary []GlossaryEntry
	for i, h := range hs {
		if h.level != level {
			continue
		}
		end := len(s.lines)
		for _, n := range hs[i+1:] {
			if n.level <= level {
				end = n.idx
				break
			}
		}
		for end > h.idx+1 && strings.TrimSpace(s.lines[end-1]) == "" {
			end--
		}
		section := s.lines[h.idx:end]
		title := reChapterNum.ReplaceAllString(h.text, "")
		switch {
		case reLegal.MatchString(title):
			legal = append(legal, section...)
			continue
		case glossaryHeading.MatchString(title):
			glossary = append(glossary, parseGlossary(section)...)
			continue
		}
		children := parseNodes(section)
		filePath := s.name
		sourcePath := ""
		if s.file != s.name {
			sourcePath = s.file
		}
		doc.Chapters = append(doc.Chapters, Chapter{
			Title:      title,
			Start:      uint(h.idx + 1),
			End:        uint(end),
			FilePath:   filePath,
			SourcePath: sourcePath,
			Commands:   commandsIn(children),
			Summary:    leadText(children),
			source:     s.file,
			prose:      proseOf(children),
		})
	}
	return legal, glossary
}

func parseNodes(lines []string) []node {
	p := parser.NewWithExtensions(parser.CommonExtensions)
	return transform(build(p.Parse([]byte(strings.Join(lines, "\n"))))).Children
}

// leadText returns the paragraphs between the chapter heading and the first
// sub heading.
func leadText(children []node) string {
	var parts []string
	for i, c := range children {
		if c.Type == "Heading" {
			if i == 0 {
				continue
			}
			break
		}
		if c.Type == "Paragraph" {
			if t := strings.TrimSpace(textOf(c, "text")); t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, " ")
}

// proseOf returns the running text of the nodes, leaving out code blocks and
// inline code so that commands like `crm configure` are not taken as mentions
// of a glossary term.
func proseOf(children []node) string {
	var b strings.Builder
	var walk func(n node)
	walk = func(n node) {
		switch n.Type {
		case "CodeBlock", "Code", "HTMLBlock", "HTMLSpan":
			return
		case "Text":
			b.WriteString(textOf(n, "text"))
		case "Paragraph", "Heading", "ListItem", "TableCell":
			defer b.WriteString("\n")
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, c := range children {
		walk(c)
	}
	return b.String()
}

func parseGlossary(lines []string) []GlossaryEntry {
	children := parseNodes(lines)
	marker := node{Type: "Heading", Content: map[string]any{"text": "glossary"}}
	var out []GlossaryEntry
	for _, c := range collapseGlossary(append([]node{marker}, children...)) {
		if c.Type == "GlossaryEntry" {
			out = append(out, GlossaryEntry{Term: textOf(c, "term"), WhatIs: textOf(c, "definition")})
		}
	}
	return out
}

var (
	// "> cmd", "alice> cmd", "admin@alice> cmd", "$ cmd"
	reUserPrompt = regexp.MustCompile(`^(?:[\w.-]+@)?[\w.-]*[>$]\s+(\S.*)$`)
	// "# cmd" (root prompt), but not "# A comment"
	reRootPrompt = regexp.MustCompile(`^#\s+([a-z0-9_./-]+(?:\s.*)?)$`)
	// interactive crm shell: "crm(live)configure# show"
	reCrmShell = regexp.MustCompile(`^crm\([^)]*\)(\w*)#\s*(.*)$`)

	// callout markers left behind by the html converter: "params foo=bar \1"
	reCallout = regexp.MustCompile(`\\\d+$`)

	crmNavigation = map[string]bool{"cd": true, "up": true, "end": true, "back": true, "quit": true, "exit": true, "bye": true, "ls": true, "help": true}
)

func commandsIn(children []node) []string {
	var blocks []string
	var walk func(n node)
	walk = func(n node) {
		if n.Type == "CodeBlock" {
			blocks = append(blocks, textOf(n, "text"))
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, c := range children {
		walk(c)
	}
	seen := map[string]bool{}
	var out []string
	for _, b := range blocks {
		for _, cmd := range commandsInBlock(b) {
			if !seen[cmd] {
				seen[cmd] = true
				out = append(out, cmd)
			}
		}
	}
	return out
}

func commandsInBlock(text string) []string {
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		cmd, ok := promptCommand(reCallout.ReplaceAllString(strings.TrimRight(lines[i], " \t"), `\`))
		if !ok {
			continue
		}
		for strings.HasSuffix(cmd, "\\") && i+1 < len(lines) {
			i++
			next := reCallout.ReplaceAllString(strings.TrimSpace(lines[i]), `\`)
			if c, ok := promptCommand(next); ok && strings.HasPrefix(next, ">") {
				next = c
			}
			cmd = strings.TrimSpace(strings.TrimSuffix(cmd, "\\")) + " " + next
		}
		cmd = strings.Join(strings.Fields(cmd), " ")
		if significant(cmd) {
			out = append(out, cmd)
		}
	}
	return out
}

func promptCommand(ln string) (string, bool) {
	if m := reCrmShell.FindStringSubmatch(ln); m != nil {
		level, args := m[1], strings.Fields(m[2])
		if len(args) == 0 || crmNavigation[args[0]] || (level == "" && len(args) == 1) {
			return "", false
		}
		return strings.Join(append([]string{"crm", level}, args...), " "), true
	}
	if m := reUserPrompt.FindStringSubmatch(ln); m != nil {
		return m[1], true
	}
	if m := reRootPrompt.FindStringSubmatch(ln); m != nil {
		return m[1], true
	}
	return "", false
}

// significant drops stray one word commands like a lone "sudo" or "exit".
func significant(cmd string) bool {
	f := strings.Fields(cmd)
	for len(f) > 0 && f[0] == "sudo" {
		f = f[1:]
	}
	return len(f) >= 2
}

var (
	reGFDL        = regexp.MustCompile(`(?i)GNU Free Documentation License`)
	reGFDLVersion = regexp.MustCompile(`(?i)\bversion\s+(1\.[123])\b`)
	reLaterVer    = regexp.MustCompile(`(?i)any later version`)
	reInvariants  = regexp.MustCompile(`(?i)with (?:the )?Invariant Sections? being`)
	reNoInvariant = regexp.MustCompile(`(?i)with no Invariant Sections`)

	otherLicenses = []struct {
		re   *regexp.Regexp
		spdx string
	}{
		{regexp.MustCompile(`(?i)Creative Commons Attribution[- ]Share[- ]?Alike 4\.0`), "CC-BY-SA-4.0"},
		{regexp.MustCompile(`(?i)Creative Commons Attribution 4\.0`), "CC-BY-4.0"},
		{regexp.MustCompile(`(?i)Apache License,? Version 2\.0`), "Apache-2.0"},
		{regexp.MustCompile(`(?i)GNU General Public License,? version 2`), "GPL-2.0-only"},
		{regexp.MustCompile(`(?i)GNU General Public License,? version 3`), "GPL-3.0-only"},
		{regexp.MustCompile(`(?i)\bMIT License\b`), "MIT"},
	}
)

// spdx maps a license notice like "GNU Free Documentation License, Version 1.2
// or (at your option) version 1.3; with the Invariant Section being ..." to an
// SPDX expression.
func spdx(notice string) string {
	if notice == "" {
		return ""
	}
	if reGFDL.MatchString(notice) {
		var versions []string
		for _, m := range reGFDLVersion.FindAllStringSubmatch(notice, -1) {
			if !contains(versions, m[1]) {
				versions = append(versions, m[1])
			}
		}
		if len(versions) == 0 {
			versions = []string{"1.3"}
		}
		inv := ""
		switch {
		case reInvariants.MatchString(notice):
			inv = "-invariants"
		case reNoInvariant.MatchString(notice):
			inv = "-no-invariants"
		}
		suffix := "-only"
		if reLaterVer.MatchString(notice) {
			suffix = "-or-later"
		}
		ids := make([]string, len(versions))
		for i, v := range versions {
			ids[i] = "GFDL-" + v + inv + suffix
		}
		return strings.Join(ids, " OR ")
	}
	for _, l := range otherLicenses {
		if l.re.MatchString(notice) {
			return l.spdx
		}
	}
	return ""
}

// spdxFromLicenseText is the fallback when there is no legal notice: the full
// license text only tells us which license and version is shipped, its
// addendum template must not be mistaken for the document's own terms.
func spdxFromLicenseText(text string) string {
	if reGFDL.MatchString(text) {
		if m := reGFDLVersion.FindStringSubmatch(text); m != nil {
			return "GFDL-" + m[1] + "-only"
		}
		return ""
	}
	return spdx(text)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

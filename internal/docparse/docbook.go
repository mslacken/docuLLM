package docparse

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// DocBook assemblies (SUSE "Smart Docs", see github.com/SUSE/doc-modular) are
// published through DC files: shell style KEY="value" files naming the
// assembly (MAIN, SRC_DIR) and the profile (PROFOS, PROFARCH, PROFCONDITION).
// Every DC file becomes one Document; its chapters are the top level modules
// of the selected <structure>, rendered to markdown so that the markdown
// based search, command and glossary extraction work unchanged.

// Debug enables diagnostic output on stderr while loading documents.
var Debug bool

func debugf(format string, args ...any) {
	if Debug {
		fmt.Fprintf(os.Stderr, "[debug] docbook: "+format+"\n", args...)
	}
}

// warnf reports content that is dropped, like the "skipping combined file"
// message of the markdown loader.
func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "docbook: "+format+"\n", args...)
}

// DocSource records where a DocBook document came from.
type DocSource struct {
	DC        string `json:"dc" yaml:"dc"`                                   // DC file, relative to the corpus root
	Assembly  string `json:"assembly" yaml:"assembly"`                       // assembly file, relative to the corpus root
	Structure string `json:"structure,omitempty" yaml:"structure,omitempty"` // xml:id of the selected <structure>
}

// Profile is the profiling a DC file applies (PROFOS, PROFARCH, PROFCONDITION).
type Profile struct {
	OS        []string `json:"os,omitempty" yaml:"os,omitempty"`
	Arch      []string `json:"arch,omitempty" yaml:"arch,omitempty"`
	Condition []string `json:"condition,omitempty" yaml:"condition,omitempty"`
}

// DocMeta is the <meta> information of an assembly.
type DocMeta struct {
	Maintainer        string   `json:"maintainer,omitempty" yaml:"maintainer,omitempty"`
	Series            string   `json:"series,omitempty" yaml:"series,omitempty"`
	Tasks             []string `json:"tasks,omitempty" yaml:"tasks,omitempty"`
	Categories        []string `json:"categories,omitempty" yaml:"categories,omitempty"`
	Architectures     []string `json:"architectures,omitempty" yaml:"architectures,omitempty"`
	Updated           string   `json:"updated,omitempty" yaml:"updated,omitempty"`
	Title             string   `json:"title,omitempty" yaml:"title,omitempty"` // title for search engines
	Description       string   `json:"description,omitempty" yaml:"description,omitempty"`
	SocialDescription string   `json:"social_description,omitempty" yaml:"social_description,omitempty"`
}

// Revision is an entry of the revision history.
type Revision struct {
	Date        string `json:"date,omitempty" yaml:"date,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Bugtracker is where documentation bugs are reported (docmanager metadata).
type Bugtracker struct {
	URL       string `json:"url,omitempty" yaml:"url,omitempty"`
	Component string `json:"component,omitempty" yaml:"component,omitempty"`
	Product   string `json:"product,omitempty" yaml:"product,omitempty"`
	Assignee  string `json:"assignee,omitempty" yaml:"assignee,omitempty"`
}

// Section is a heading below the chapter heading. Level 1 is a direct
// subsection of the chapter. File is set for sections taken from another
// topic file (nested modules).
type Section struct {
	ID    string `json:"id,omitempty" yaml:"id,omitempty"`
	Title string `json:"title" yaml:"title"`
	Level int    `json:"level" yaml:"level"`
	File  string `json:"file,omitempty" yaml:"file,omitempty"`
}

// Procedure is a numbered list of steps.
type Procedure struct {
	ID    string `json:"id,omitempty" yaml:"id,omitempty"`
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
	Steps int    `json:"steps" yaml:"steps"`
}

// Admonition is a note, tip, important, warning or caution box.
type Admonition struct {
	Type  string `json:"type" yaml:"type"`
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
}

// Xref is a cross reference; Resolved is false if the target is not part of
// the document.
type Xref struct {
	Linkend  string `json:"linkend" yaml:"linkend"`
	Resolved bool   `json:"resolved" yaml:"resolved"`
}

// Link is an external link.
type Link struct {
	URL  string `json:"url" yaml:"url"`
	Text string `json:"text,omitempty" yaml:"text,omitempty"`
}

// Image is an image shown in the chapter; File is relative to the corpus root.
type Image struct {
	File string `json:"file" yaml:"file"`
	Alt  string `json:"alt,omitempty" yaml:"alt,omitempty"`
}

// dcFiles returns the names of the DC files at the top level of dir, sorted.
// Disabled ones (_DC-*) do not match.
func dcFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "DC-") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

var reDCLine = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$`)

// readDC reads the KEY="value" assignments of a DC file.
func readDC(p string) (map[string]string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	vars := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := reDCLine.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		v := m[2]
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		vars[m[1]] = v
	}
	return vars, sc.Err()
}

// dcDocID derives the document id from the DC file name:
// "DC-SLES-kdump" -> "sles-kdump".
func dcDocID(name string) string {
	return slug(strings.TrimPrefix(name, "DC-"))
}

// loadDocBook builds the corpus from the given DC files in root.
func loadDocBook(root string, dcs []string) (*Corpus, error) {
	l := newXMLLoader(root)
	c := &Corpus{Documents: []Document{}, lines: map[string][]string{}}
	ids := map[string]int{}
	for _, name := range dcs {
		doc, glossary, lines, err := l.buildDocBook(name)
		if err != nil {
			warnf("skipping %s: %v", name, err)
			continue
		}
		doc.ID = uniqueID(ids, dcDocID(name))
		c.lines[name] = lines
		c.Documents = append(c.Documents, doc)
		c.addGlossary(glossary)
	}
	if len(c.Documents) == 0 {
		return nil, fmt.Errorf("no usable DC files found in %s", root)
	}
	c.finish()
	return c, nil
}

// dbModule is a <module> of an assembly structure with its topic loaded.
type dbModule struct {
	id       string // xml:id of the topic, or of the module for containers
	title    string
	abstract *xnode // merge override or topic abstract, nil if none
	renderAs string
	file     string // topic file relative to the corpus root, "" for containers
	topic    *xnode // profiled topic root, nil for containers
	legacy   []string
	kids     []*dbModule
}

// buildDocBook turns the DC file name into a Document, the glossary entries
// it contains and the markdown lines of its chapters.
func (l *xmlLoader) buildDocBook(name string) (Document, []GlossaryEntry, []string, error) {
	vars, err := readDC(filepath.Join(l.root, name))
	if err != nil {
		return Document{}, nil, nil, err
	}
	if vars["MAIN"] == "" {
		return Document{}, nil, nil, fmt.Errorf("no MAIN")
	}
	asmPath := filepath.Join(l.root, vars["SRC_DIR"], vars["MAIN"])
	f, err := l.load(asmPath)
	if err != nil {
		return Document{}, nil, nil, err
	}
	prof := profile{os: profileValues(vars["PROFOS"]), arch: profileValues(vars["PROFARCH"]), condition: profileValues(vars["PROFCONDITION"])}
	asm := prof.apply(f.root)
	if asm == nil || asm.name != "assembly" {
		return Document{}, nil, nil, fmt.Errorf("%s is not an assembly", l.rel(asmPath))
	}
	st := selectStructure(asm, firstNonEmpty(vars["STRUCTID"], vars["ROOTID"]))
	if st == nil {
		return Document{}, nil, nil, fmt.Errorf("%s has no structure", l.rel(asmPath))
	}
	resources := map[string]string{}
	for _, rs := range asm.children("resources") {
		for _, r := range rs.children("resource") {
			if id, href := r.attr["id"], r.attr["href"]; id != "" && href != "" {
				resources[id] = filepath.Join(filepath.Dir(asmPath), href)
			}
		}
	}
	doc := Document{
		Chapters: []Chapter{},
		Source:   &DocSource{DC: name, Assembly: l.rel(asmPath), Structure: st.attr["id"]},
		Profile:  &Profile{OS: prof.os, Arch: prof.arch, Condition: prof.condition},
	}
	merge := st.child("merge")
	addAssemblyMeta(&doc, merge)
	if doc.Distro.Product == "" {
		doc.Distro.Product = entityText(f.ents, "productname", prof)
	}
	if len(doc.Distro.Versions) == 0 {
		if v := entityText(f.ents, "productnumber", prof); v != "" {
			doc.Distro.Versions = []string{v}
		}
	}
	if doc.Distro.Product == "" && doc.Bugtracker != nil {
		doc.Distro.Product = reTrailingVersion.ReplaceAllString(doc.Bugtracker.Product, "")
	}
	addProfileVersions(&doc.Distro, prof)
	modules := l.modules(st, resources, prof)

	r := newRenderer(vars["IMG_SRC_DIR"])
	r.collectIDs(st.attr["id"], doc.Title, modules)
	glossary := r.glossary(modules)
	if summary := r.text(leadParas(merge.child("abstract"))); summary != "" {
		doc.Summary = summary
	}

	var lines, legal, license []string
	chapterIDs := map[string]int{}
	for _, m := range modules {
		switch {
		case m.topic != nil && reLegal.MatchString(m.title):
			legal = append(legal, m.topic.flatText())
		case m.topic != nil && reLicenseTitle.MatchString(m.title):
			license = append(license, m.topic.flatText())
		case m.renderAs == "glossary" || isGlossaryTopic(m.topic):
		default:
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			ch, text := r.chapter(m, len(lines))
			lines = append(lines, text...)
			ch.ID = uniqueID(chapterIDs, firstNonEmpty(m.id, slug(m.title)))
			ch.source = name
			doc.Chapters = append(doc.Chapters, ch)
		}
	}
	if doc.Summary == "" && doc.Meta != nil {
		doc.Summary = doc.Meta.Description
	}
	if doc.Summary == "" && len(doc.Chapters) > 0 {
		doc.Summary = doc.Chapters[0].Summary
	}
	doc.License = spdx(strings.Join(legal, "\n"))
	if doc.License == "" {
		doc.License = spdxFromLicenseText(strings.Join(license, "\n"))
	}
	return doc, glossary, lines, nil
}

// selectStructure returns the structure with the given id, or the first one.
func selectStructure(asm *xnode, id string) *xnode {
	structs := asm.children("structure")
	if len(structs) == 0 {
		return nil
	}
	for _, s := range structs {
		if id != "" && s.attr["id"] == id {
			return s
		}
	}
	if id != "" {
		warnf("structure %q not found, using %q", id, structs[0].attr["id"])
	}
	return structs[0]
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

var reTrailingVersion = regexp.MustCompile(`\s+\d+(?:\.\d+)*(?:\s+SP\d+)?$`)
var reVersion = regexp.MustCompile(`^\d+(?:\.\d+)+$`)

// addAssemblyMeta fills the document metadata from the <merge> of the structure.
func addAssemblyMeta(doc *Document, merge *xnode) {
	doc.Meta = &DocMeta{}
	doc.Title = merge.child("title").flatText()
	doc.Subtitle = merge.child("subtitle").flatText()
	for _, m := range merge.children("meta") {
		text := firstNonEmpty(m.attr["content"], m.flatText())
		switch m.attr["name"] {
		case "maintainer":
			doc.Meta.Maintainer = text
		case "series":
			doc.Meta.Series = text
		case "task":
			doc.Meta.Tasks = phrases(m)
		case "category":
			doc.Meta.Categories = phrases(m)
		case "architecture":
			doc.Meta.Architectures = phrases(m)
		case "updated":
			doc.Meta.Updated = text
		case "title":
			doc.Meta.Title = text
		case "description":
			doc.Meta.Description = text
		case "social-descr":
			doc.Meta.SocialDescription = text
		case "productname":
			for _, p := range m.find("productname") {
				if doc.Distro.Product == "" {
					doc.Distro.Product = p.flatText()
				}
				if v := p.attr["version"]; v != "" && !contains(doc.Distro.Versions, v) {
					doc.Distro.Versions = append(doc.Distro.Versions, v)
				}
			}
		}
	}
	if doc.Title == "" {
		doc.Title = doc.Meta.Title
	}
	for _, rev := range merge.find("revision") {
		doc.Revisions = append(doc.Revisions, Revision{
			Date:        rev.child("date").flatText(),
			Description: firstNonEmpty(rev.child("revdescription").flatText(), rev.child("revremark").flatText()),
		})
	}
	if dm := merge.find("docmanager"); len(dm) > 0 {
		if bt := dm[0].child("bugtracker"); bt != nil {
			doc.Bugtracker = &Bugtracker{
				URL: bt.child("url").flatText(), Component: bt.child("component").flatText(),
				Product: bt.child("product").flatText(), Assignee: bt.child("assignee").flatText(),
			}
		}
		doc.Translation = dm[0].child("translation").flatText()
	}
	if reflect.DeepEqual(*doc.Meta, DocMeta{}) {
		doc.Meta = nil
	}
}

// addProfileVersions adds version numbers selected by PROFCONDITION, like 16.0.
func addProfileVersions(d *Distro, prof profile) {
	for _, c := range prof.condition {
		if reVersion.MatchString(c) && !contains(d.Versions, c) {
			d.Versions = append(d.Versions, c)
		}
	}
}

// entityText returns the profiled text of the entity name, e.g. the product
// name for the DC's PROFOS out of the per product phrases of &productname;.
func entityText(t *entityTable, name string, prof profile) string {
	if t == nil || t.values[name] == "" {
		return ""
	}
	n, err := decodeXML("<phrase>" + t.expand("&"+name+";") + "</phrase>")
	if err != nil {
		debugf("entity %s: %v", name, err)
		return ""
	}
	return prof.apply(n).flatText()
}

// phrases returns the texts of the <phrase>s of a meta element, or its text if
// it has none.
func phrases(m *xnode) []string {
	var out []string
	for _, p := range m.children("phrase") {
		if t := p.flatText(); t != "" && !contains(out, t) {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		if t := m.flatText(); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// leadParas returns the leading paragraphs of an abstract, stopping at the
// first other block or a paragraph that is just emphasis (a pseudo heading
// like "Prerequisites").
func leadParas(abstract *xnode) *xnode {
	out := &xnode{name: "abstract"}
	if abstract == nil {
		return out
	}
	for _, k := range abstract.kids {
		if k.name == "" {
			continue
		}
		if k.name != "para" || isPseudoHeading(k) {
			break
		}
		out.kids = append(out.kids, k)
	}
	return out
}

func isPseudoHeading(p *xnode) bool {
	var elems []*xnode
	for _, k := range p.kids {
		if k.name != "" {
			elems = append(elems, k)
		} else if strings.TrimSpace(k.text) != "" {
			return false
		}
	}
	return len(elems) == 1 && elems[0].name == "emphasis"
}

// modules loads the modules below parent (a structure or module).
func (l *xmlLoader) modules(parent *xnode, resources map[string]string, prof profile) []*dbModule {
	var out []*dbModule
	for _, mod := range parent.children("module") {
		m := &dbModule{id: mod.attr["id"], renderAs: firstNonEmpty(mod.attr["renderas"], mod.child("output").attrOr("renderas"), "section")}
		merge := mod.child("merge")
		if ref := mod.attr["resourceref"]; ref != "" {
			p, ok := resources[ref]
			if !ok {
				warnf("module refers to unknown resource %q", ref)
			} else if f, err := l.load(p); err != nil {
				warnf("module %q: %v", ref, err)
			} else if t := prof.apply(f.root); t != nil {
				m.topic, m.file, m.legacy = t, l.rel(p), f.legacy
				m.id = firstNonEmpty(t.attr["id"], m.id)
				m.title = t.titleNode().flatText()
				m.abstract = t.info("abstract")
			}
		}
		if t := merge.child("title").flatText(); t != "" {
			m.title = t
		}
		if a := merge.child("abstract"); a != nil {
			m.abstract = a
		}
		m.kids = l.modules(mod, resources, prof)
		if m.topic == nil && m.title == "" && len(m.kids) == 0 {
			continue
		}
		out = append(out, m)
	}
	return out
}

// attrOr returns the attribute name of n, or "" for a nil node.
func (n *xnode) attrOr(name string) string {
	if n == nil {
		return ""
	}
	return n.attr[name]
}

// isGlossaryTopic reports whether the topic consists of glossary entries only.
func isGlossaryTopic(t *xnode) bool {
	if t == nil {
		return false
	}
	entries := false
	for _, k := range t.kids {
		switch k.name {
		case "", "info", "title", "abstract":
		case "glossentry", "glossdiv", "glossary", "glosslist":
			entries = true
		default:
			return false
		}
	}
	return entries
}

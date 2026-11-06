package docparse

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// xnode is an element or text node of a decoded DocBook file.
type xnode struct {
	name string            // local element name, "" for text
	attr map[string]string // attributes by local name ("id" for xml:id)
	text string            // content of a text node
	kids []*xnode
}

// xfile is a parsed DocBook file: entities expanded, XIncludes resolved, not
// yet profiled.
type xfile struct {
	root   *xnode
	legacy []string     // URLs from "refers to ..." comments pointing at the legacy source
	ents   *entityTable // entities visible in the file
}

const xincludeNS = "http://www.w3.org/2001/XInclude"

// includeNode is the name of the node replacing an <xi:include/>; its href
// attribute is the included file relative to the corpus root and its only
// child the root element of that file.
const includeNode = "#include"

// xmlLoader parses DocBook files once and caches them, as topics and
// snippets are shared by many assemblies.
type xmlLoader struct {
	root    string // corpus root, file names are reported relative to it
	ents    *entityLoader
	files   map[string]*xfile
	errs    map[string]error
	loading map[string]bool // files being loaded, to break include cycles
}

func newXMLLoader(root string) *xmlLoader {
	return &xmlLoader{root: root, ents: newEntityLoader(), files: map[string]*xfile{}, errs: map[string]error{}, loading: map[string]bool{}}
}

// rel returns p relative to the corpus root, with forward slashes.
func (l *xmlLoader) rel(p string) string {
	if r, err := filepath.Rel(l.root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(p)
}

// load returns the parsed file p.
func (l *xmlLoader) load(p string) (*xfile, error) {
	p = filepath.Clean(p)
	if f, ok := l.files[p]; ok {
		return f, nil
	}
	if err, ok := l.errs[p]; ok {
		return nil, err
	}
	if l.loading[p] {
		return nil, fmt.Errorf("%s includes itself", l.rel(p))
	}
	l.loading[p] = true
	defer delete(l.loading, p)
	f, err := l.parse(p)
	if err != nil {
		l.errs[p] = err
		return nil, err
	}
	l.files[p] = f
	return f, nil
}

func (l *xmlLoader) parse(p string) (*xfile, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	raw := string(data)
	body, subset := splitDoctype(raw)
	ents := l.ents.table(filepath.Dir(p), subset)
	text := ents.expand(body)
	root, err := decodeXML(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", l.rel(p), err)
	}
	l.resolveIncludes(root, filepath.Dir(p))
	return &xfile{root: root, legacy: legacySources(raw), ents: ents}, nil
}

// decodeXML builds the node tree of the root element of doc. Comments,
// processing instructions and directives are dropped.
func decodeXML(doc string) (*xnode, error) {
	d := xml.NewDecoder(strings.NewReader(strings.TrimLeft(doc, "\uFEFF \t\r\n")))
	var stack []*xnode
	var root *xnode
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xnode{name: t.Name.Local, attr: map[string]string{}}
			if t.Name.Space == xincludeNS && t.Name.Local == "include" {
				n.name = "xi:include"
			}
			for _, a := range t.Attr {
				if a.Name.Space != "xmlns" && a.Name.Local != "xmlns" {
					n.attr[a.Name.Local] = a.Value
				}
			}
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				top.kids = append(top.kids, n)
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				top.kids = append(top.kids, &xnode{text: string(t)})
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("no root element")
	}
	return root, nil
}

// resolveIncludes replaces every <xi:include href="..."/> below n by an
// includeNode holding the root of the included file.
func (l *xmlLoader) resolveIncludes(n *xnode, dir string) {
	for i, k := range n.kids {
		if k.name != "xi:include" {
			l.resolveIncludes(k, dir)
			continue
		}
		href := k.attr["href"]
		if href == "" || k.attr["parse"] == "text" {
			debugf("unsupported xinclude %q in %s", href, l.rel(dir))
			n.kids[i] = &xnode{name: includeNode, attr: map[string]string{"href": href}}
			continue
		}
		p := filepath.Join(dir, href)
		inc := &xnode{name: includeNode, attr: map[string]string{"href": l.rel(p)}}
		if f, err := l.load(p); err != nil {
			warnf("xinclude: %v", err)
		} else {
			inc.kids = []*xnode{f.root}
		}
		n.kids[i] = inc
	}
}

var (
	reComment = regexp.MustCompile(`(?s)<!--(.*?)-->`)
	reRefers  = regexp.MustCompile(`(?i)\brefers? to\b`)
	reURL     = regexp.MustCompile(`https?://[^\s<>"'\]\[]+`)
)

// legacySources returns the URLs of "<!-- refers to legacy doc: URL -->"
// comments, which point to the file the topic was taken from.
func legacySources(raw string) []string {
	var out []string
	for _, m := range reComment.FindAllStringSubmatch(raw, -1) {
		if !reRefers.MatchString(m[1]) {
			continue
		}
		for _, u := range reURL.FindAllString(m[1], -1) {
			if !contains(out, u) {
				out = append(out, u)
			}
		}
	}
	return out
}

// profile holds the values a DC file selects with PROFOS, PROFARCH and
// PROFCONDITION.
type profile struct {
	os, arch, condition []string
}

// profileValues splits a ";" separated profiling value and normalises the
// spelling of architectures.
func profileValues(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ";") {
		v = strings.ToLower(strings.TrimSpace(v))
		switch v {
		case "":
			continue
		case "x86-64", "amd64":
			v = "x86_64"
		case "arm", "arm64":
			v = "aarch64"
		}
		out = append(out, v)
	}
	return out
}

// keep reports whether element n is part of the profiled output: every
// profiling attribute it has must share a value with the profile, unless the
// profile does not set that dimension.
func (p profile) keep(n *xnode) bool {
	for _, d := range []struct {
		attr string
		want []string
	}{{"os", p.os}, {"arch", p.arch}, {"condition", p.condition}} {
		v, ok := n.attr[d.attr]
		if !ok || len(d.want) == 0 {
			continue
		}
		match := false
		for _, have := range profileValues(v) {
			match = match || contains(d.want, have)
		}
		if !match {
			return false
		}
	}
	return true
}

// apply returns a copy of n without the elements the profile excludes, or nil
// if n itself is excluded. Text nodes are shared.
func (p profile) apply(n *xnode) *xnode {
	if n == nil || n.name == "" {
		return n
	}
	if !p.keep(n) {
		return nil
	}
	out := &xnode{name: n.name, attr: n.attr, kids: make([]*xnode, 0, len(n.kids))}
	for _, k := range n.kids {
		if c := p.apply(k); c != nil {
			out.kids = append(out.kids, c)
		}
	}
	return out
}

// child returns the first child element of n called name, or nil.
func (n *xnode) child(name string) *xnode {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.name == name {
			return k
		}
	}
	return nil
}

// children returns the child elements of n called name.
func (n *xnode) children(name string) []*xnode {
	var out []*xnode
	if n == nil {
		return out
	}
	for _, k := range n.kids {
		if k.name == name {
			out = append(out, k)
		}
	}
	return out
}

// find returns all elements called name below n, in document order, not
// descending into matches.
func (n *xnode) find(name string) []*xnode {
	var out []*xnode
	if n == nil {
		return out
	}
	for _, k := range n.kids {
		if k.name == name {
			out = append(out, k)
		} else {
			out = append(out, k.find(name)...)
		}
	}
	return out
}

// titleNode returns the title of n, either a direct child or in its info.
func (n *xnode) titleNode() *xnode {
	if t := n.child("title"); t != nil {
		return t
	}
	return n.child("info").child("title")
}

// info returns the child element called name of n or of its info element.
func (n *xnode) info(name string) *xnode {
	if c := n.child("info").child(name); c != nil {
		return c
	}
	return n.child(name)
}

// rawText returns all text below n, dropping editorial remarks and callout
// markers.
func (n *xnode) rawText() string {
	if n == nil {
		return ""
	}
	if n.name == "" {
		return n.text
	}
	if n.name == "remark" || n.name == "co" {
		return ""
	}
	var b strings.Builder
	for _, k := range n.kids {
		b.WriteString(k.rawText())
	}
	return b.String()
}

// flatText returns the text below n with white space collapsed.
func (n *xnode) flatText() string {
	return strings.Join(strings.Fields(n.rawText()), " ")
}

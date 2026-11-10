package docbook

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Go's encoding/xml does not read DTDs, so the entities DocBook files pull in
// via their internal subset (`<!ENTITY % entities SYSTEM "../common/generic-entities.ent"> %entities;`)
// are resolved here by plain textual substitution before the XML is decoded.

// entityTable holds the general entities visible to a file. As in XML, the
// first declaration of a name wins.
type entityTable struct {
	values   map[string]string // name -> replacement text as declared
	params   map[string]paramEntity
	expanded map[string]string // memoized recursive expansion of values
}

// paramEntity is a parameter entity; file is set for SYSTEM entities.
type paramEntity struct {
	value, file string
}

// entityLoader caches the entity files and the tables built from them. Nearly
// every file has the same internal subset, so the tables are keyed by subset
// text and directory.
type entityLoader struct {
	files  map[string]string
	tables map[string]*entityTable
}

func newEntityLoader() *entityLoader {
	return &entityLoader{files: map[string]string{}, tables: map[string]*entityTable{}}
}

// table returns the entities declared by subset, the internal DTD subset of a
// file in dir.
func (l *entityLoader) table(dir, subset string) *entityTable {
	key := dir + "\x00" + subset
	if t, ok := l.tables[key]; ok {
		return t
	}
	t := &entityTable{values: map[string]string{}, params: map[string]paramEntity{}, expanded: map[string]string{}}
	l.parseDTD(t, subset, dir, 0)
	l.tables[key] = t
	return t
}

func (l *entityLoader) readFile(p string) (string, bool) {
	if s, ok := l.files[p]; ok {
		return s, true
	}
	data, err := os.ReadFile(p)
	if err != nil {
		debugf("entity file %s: %v", p, err)
		return "", false
	}
	l.files[p] = string(data)
	return l.files[p], true
}

var (
	reEntityDecl = regexp.MustCompile(`^<!ENTITY\s+(%\s+)?([^\s%;]+)\s+`)
	reSystemID   = regexp.MustCompile(`^(?:SYSTEM|PUBLIC\s+(?:"[^"]*"|'[^']*'))\s+(?:"([^"]*)"|'([^']*)')`)
	reParamRef   = regexp.MustCompile(`^%([^\s%;]+);`)
)

// parseDTD reads the entity declarations in text. Parameter entity references
// are followed, SYSTEM files are resolved relative to dir; all other
// declarations are skipped.
func (l *entityLoader) parseDTD(t *entityTable, text, dir string, depth int) {
	if depth > 16 {
		debugf("parameter entities nested too deep in %s", dir)
		return
	}
	for i := 0; i < len(text); {
		rest := text[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			i += skipPast(rest, "-->")
		case strings.HasPrefix(rest, "<!ENTITY"):
			i += l.entityDecl(t, rest, dir)
		case strings.HasPrefix(rest, "<?"):
			i += skipPast(rest, "?>")
		case strings.HasPrefix(rest, "<!"):
			i += declEnd(rest)
		case rest[0] == '%':
			m := reParamRef.FindStringSubmatch(rest)
			if m == nil {
				i++
				continue
			}
			i += len(m[0])
			p, ok := t.params[m[1]]
			switch {
			case !ok:
				debugf("undefined parameter entity %%%s; in %s", m[1], dir)
			case p.file != "":
				if s, ok := l.readFile(p.file); ok {
					l.parseDTD(t, s, filepath.Dir(p.file), depth+1)
				}
			default:
				l.parseDTD(t, p.value, dir, depth+1)
			}
		default:
			i++
		}
	}
}

// entityDecl records the <!ENTITY ...> declaration at the start of s and
// returns its length.
func (l *entityLoader) entityDecl(t *entityTable, s, dir string) int {
	n := declEnd(s)
	m := reEntityDecl.FindStringSubmatch(s[:n])
	if m == nil {
		return n
	}
	param, name, def := m[1] != "", m[2], s[len(m[0]):n]
	var e paramEntity
	if v, ok := quoted(def); ok {
		e.value = v
	} else if sm := reSystemID.FindStringSubmatch(def); sm != nil {
		e.file = filepath.Join(dir, sm[1]+sm[2])
	} else {
		return n
	}
	if param {
		if _, ok := t.params[name]; !ok {
			t.params[name] = e
		}
		return n
	}
	if _, ok := t.values[name]; !ok && e.file == "" {
		t.values[name] = e.value
	}
	return n
}

// quoted returns the content of the quoted literal at the start of s.
func quoted(s string) (string, bool) {
	if s == "" || (s[0] != '"' && s[0] != '\'') {
		return "", false
	}
	end := strings.IndexByte(s[1:], s[0])
	if end < 0 {
		return "", false
	}
	return s[1 : end+1], true
}

// skipPast returns the length of s up to and including the end marker, or
// len(s) if there is none.
func skipPast(s, end string) int {
	if i := strings.Index(s, end); i >= 0 {
		return i + len(end)
	}
	return len(s)
}

// declEnd returns the length of the markup declaration at the start of s,
// up to and including its closing '>', skipping quoted literals.
func declEnd(s string) int {
	var q byte
	for i := 1; i < len(s); i++ {
		switch c := s[i]; {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '>':
			return i + 1
		}
	}
	return len(s)
}

// splitDoctype removes the <!DOCTYPE ...> declaration from doc and returns the
// document without it and the internal subset (the part in square brackets).
func splitDoctype(doc string) (string, string) {
	start := strings.Index(doc, "<!DOCTYPE")
	if start < 0 {
		return doc, ""
	}
	var q byte
	subStart, subEnd := -1, -1
	for i := start + len("<!DOCTYPE"); i < len(doc); i++ {
		c := doc[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case subStart >= 0 && subEnd < 0 && strings.HasPrefix(doc[i:], "<!--"):
			i += skipPast(doc[i:], "-->") - 1
		case c == '"' || c == '\'':
			q = c
		case c == '[' && subStart < 0:
			subStart = i + 1
		case c == ']' && subStart >= 0 && subEnd < 0:
			subEnd = i
		case c == '>' && (subStart < 0 || subEnd >= 0):
			subset := ""
			if subStart >= 0 {
				subset = doc[subStart:subEnd]
			}
			return doc[:start] + doc[i+1:], subset
		}
	}
	return doc, ""
}

var (
	reEntityRef = regexp.MustCompile(`&([A-Za-z_:][\w.:-]*);`)
	predefined  = map[string]bool{"lt": true, "gt": true, "amp": true, "quot": true, "apos": true}
)

// expand replaces the entity references in the XML text doc by their
// (recursively expanded) values. Comments and CDATA sections are left alone,
// as are the predefined XML entities. Undefined entities are escaped so that
// they show up literally instead of breaking the XML decoder.
func (t *entityTable) expand(doc string) string {
	var b strings.Builder
	for doc != "" {
		i := strings.IndexAny(doc, "&<")
		if i < 0 {
			b.WriteString(doc)
			break
		}
		b.WriteString(doc[:i])
		doc = doc[i:]
		var n int
		switch {
		case strings.HasPrefix(doc, "<!--"):
			n = skipPast(doc, "-->")
			b.WriteString(doc[:n])
		case strings.HasPrefix(doc, "<![CDATA["):
			n = skipPast(doc, "]]>")
			b.WriteString(doc[:n])
		case doc[0] == '&':
			m := reEntityRef.FindStringSubmatch(doc)
			if m == nil {
				n = 1
				b.WriteByte('&')
			} else {
				n = len(m[0])
				b.WriteString(t.value(m[1], map[string]bool{}))
			}
		default:
			n = 1
			b.WriteByte('<')
		}
		doc = doc[n:]
	}
	return b.String()
}

// value returns the fully expanded replacement text of the entity name.
// active holds the entities being expanded, to break reference cycles.
func (t *entityTable) value(name string, active map[string]bool) string {
	if predefined[name] {
		return "&" + name + ";"
	}
	if v, ok := t.expanded[name]; ok {
		return v
	}
	v, ok := t.values[name]
	if !ok {
		debugf("undefined entity &%s;", name)
		t.expanded[name] = "&amp;" + name + ";"
		return t.expanded[name]
	}
	if active[name] {
		debugf("recursive entity &%s;", name)
		return "&amp;" + name + ";"
	}
	active[name] = true
	out := reEntityRef.ReplaceAllStringFunc(v, func(ref string) string {
		return t.value(ref[1:len(ref)-1], active)
	})
	delete(active, name)
	t.expanded[name] = out
	return out
}

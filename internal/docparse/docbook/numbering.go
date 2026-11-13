package docbook

import (
	"strconv"
	"strings"
)

// numbering labels chapters, sections and formal objects like the SUSE
// DocBook stylesheets do: chapters are counted through a book (parts
// included), appendices get letters and parts roman numbers. Sections and
// formal objects are numbered below their chapter or appendix ("19.6.9",
// "Figure 19.1"), or from 1 in an article or preface ("3.2", "Procedure 4").
// Prefaces themselves, glossaries and their content are not numbered.
type numbering struct {
	book                        *xnode // book being numbered, see dbModule.book
	chapters, appendices, parts int
	label                       string         // label of the current chapter or appendix, "" in an article
	off                         int            // section level = depth + off
	sects                       []int          // section counters by level
	article                     []int          // section counters of the article, kept while in an appendix, glossary, ...
	formal                      map[string]int // counters of figures, tables, examples and procedures
	none                        bool           // inside a glossary, part intro, ...
}

func newNumbering() numbering {
	// the modules of an assembly article are its top level sections
	return numbering{off: 1, formal: map[string]int{}}
}

// component starts a chapter, appendix, article, ... at depth with the given
// label; its sections are numbered from depth+1 on.
func (n *numbering) component(label string, depth int, none bool) {
	if n.label == "" && n.off == 1 && !n.none {
		n.article = n.sects
	}
	n.label, n.off, n.none = label, -depth, none
	n.sects = nil
	n.formal = map[string]int{}
}

// number returns the label of module m at depth and moves on to it.
func (r *renderer) number(m *dbModule, depth int) string {
	n := &r.num
	if m.book != n.book {
		n.book, n.chapters, n.appendices, n.parts = m.book, 0, 0, 0
	}
	switch m.renderAs {
	case "chapter", "simplechapter":
		n.chapters++
		n.component(strconv.Itoa(n.chapters), depth, false)
		return n.label
	case "appendix":
		n.appendices++
		n.component(letters(n.appendices), depth, false)
		return n.label
	case "part":
		n.parts++
		n.component("", depth, true)
		return roman(n.parts)
	case "article", "preface":
		n.component("", depth, false)
		return ""
	case "glossary", "bibliography", "index", "colophon", "dedication", "acknowledgements":
		n.component("", depth, true)
		return ""
	}
	if depth == 0 && n.off != 1 {
		// a top level section of an article after an unnumbered glossary or
		// the like: back to the article's section numbering
		n.label, n.off, n.none, n.sects = "", 1, false, n.article
	}
	return r.section(depth)
}

// section returns the number of a section at depth and moves on to it.
func (r *renderer) section(depth int) string {
	n := &r.num
	level := depth + n.off
	if n.none || level < 1 {
		return ""
	}
	for len(n.sects) < level {
		n.sects = append(n.sects, 0)
	}
	n.sects = n.sects[:level]
	n.sects[level-1]++
	parts := make([]string, 0, level+1)
	if n.label != "" {
		parts = append(parts, n.label)
	}
	for _, s := range n.sects {
		parts = append(parts, strconv.Itoa(s))
	}
	return strings.Join(parts, ".")
}

// formal returns the number of the next figure, table, example or procedure
// (kind), "" where formal objects are not numbered.
func (r *renderer) formal(kind string) string {
	n := &r.num
	if n.none {
		return ""
	}
	n.formal[kind]++
	num := strconv.Itoa(n.formal[kind])
	if n.label != "" {
		return n.label + "." + num
	}
	return num
}

// letters returns the label of the i-th appendix: A, B, ..., Z, AA, AB, ...
func letters(i int) string {
	s := ""
	for ; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// roman returns i as roman numeral, the label of the i-th part.
func roman(i int) string {
	var b strings.Builder
	for _, d := range []struct {
		v int
		s string
	}{{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"}, {100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}} {
		for ; i >= d.v; i -= d.v {
			b.WriteString(d.s)
		}
	}
	return b.String()
}

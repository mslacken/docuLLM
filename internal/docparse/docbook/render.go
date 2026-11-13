package docbook

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"docullm/internal/docparse"
	"docullm/internal/docparse/markdown"
)

// renderer turns profiled DocBook topics into markdown and records the
// structured information (sections, procedures, links, ...) of the chapter
// being rendered.
type renderer struct {
	imgDir   string            // IMG_SRC_DIR, images are referenced relative to it
	ids      map[string]string // xml:id -> title of every element of the document, for xrefs
	callouts map[string]int    // xml:id of a <co> -> its number within its screen
	num      numbering
	ch       *docparse.Chapter // chapter being rendered, nil while rendering other text
	alt      string            // fallback alt text for images in a figure
}

func newRenderer(imgDir string) *renderer {
	if imgDir == "" {
		imgDir = "images"
	}
	return &renderer{imgDir: imgDir, ids: map[string]string{}, callouts: map[string]int{}, num: newNumbering()}
}

// collectIDs records the ids and titles of the structure and all modules, so
// that xrefs across topics get their target's title.
func (r *renderer) collectIDs(structID, title string, modules []*dbModule) {
	if structID != "" {
		r.ids[structID] = title
	}
	for _, m := range modules {
		if m.topic != nil {
			r.collectNodeIDs(m.topic)
		}
		if m.id != "" {
			r.ids[m.id] = m.title // the merge title overrides the topic title
		}
		r.collectIDs("", "", m.kids)
	}
}

func (r *renderer) collectNodeIDs(n *xnode) {
	if id := n.attr["id"]; id != "" {
		if _, ok := r.ids[id]; !ok {
			r.ids[id] = idTitle(n)
		}
	}
	for _, k := range n.kids {
		if k.name != "" {
			r.collectNodeIDs(k)
		}
	}
}

// idTitle is the text an xref to n shows.
func idTitle(n *xnode) string {
	switch n.name {
	case "glossentry":
		return n.child("glossterm").flatText()
	case "varlistentry":
		return n.child("term").flatText()
	}
	return n.titleNode().flatText()
}

// glossary returns the glossary entries of all modules.
func (r *renderer) glossary(modules []*dbModule) []docparse.GlossaryEntry {
	var out []docparse.GlossaryEntry
	for _, m := range modules {
		for _, e := range m.topic.find("glossentry") {
			term, def := e.child("glossterm").flatText(), r.text(e.child("glossdef"))
			if term != "" && def != "" {
				out = append(out, docparse.GlossaryEntry{Term: term, WhatIs: def})
			}
		}
		out = append(out, r.glossary(m.kids)...)
	}
	return out
}

// chapter renders the top level module m. offset is the number of lines of
// the document before the chapter, for Start and End.
func (r *renderer) chapter(m *dbModule, offset int) (docparse.Chapter, []string) {
	ch := docparse.Chapter{Title: m.title, FilePath: m.file, RenderAs: m.renderAs, TopicType: "container", LegacySources: m.legacy}
	if m.topic != nil {
		ch.TopicType = m.topic.attr["role"]
		ch.Maintainer = maintainer(m.topic)
	}
	r.ch = &ch
	lines := r.module(m, 2, 0)
	r.ch = nil
	ch.Start, ch.End = uint(offset+1), uint(offset+len(lines))
	a := markdown.Analyze(lines)
	ch.Commands, ch.Prose = a.Commands, a.Prose
	ch.Summary = firstNonEmpty(r.text(m.abstract), a.Lead)
	return ch, lines
}

func maintainer(topic *xnode) string {
	for _, m := range topic.child("info").children("meta") {
		if m.attr["name"] == "maintainer" {
			return firstNonEmpty(m.attr["content"], m.flatText())
		}
	}
	return ""
}

// module renders a module and its nested modules; level is the markdown
// heading level, depth the section depth within the chapter.
func (r *renderer) module(m *dbModule, level, depth int) []string {
	num := r.number(m, depth)
	bs := [][]string{{mdHeading(level, num, m.title, m.id)}}
	if depth == 0 {
		r.ch.Number = num
	} else {
		r.ch.Sections = append(r.ch.Sections, docparse.Section{ID: m.id, Number: num, Title: m.title, Level: depth, File: m.file})
		for _, u := range m.legacy {
			if !slices.Contains(r.ch.LegacySources, u) {
				r.ch.LegacySources = append(r.ch.LegacySources, u)
			}
		}
	}
	if m.abstract != nil {
		bs = append(bs, r.blocks(m.abstract.kids, level+1, depth+1)...)
	}
	if m.topic != nil {
		bs = append(bs, r.blocks(r.without(m.topic.kids, "info", "title", "subtitle", "titleabbrev", "abstract"), level+1, depth+1)...)
	}
	for _, k := range m.kids {
		bs = append(bs, r.module(k, level+1, depth+1))
	}
	return joinBlocks(bs)
}

func mdHeading(level int, num, title, id string) string {
	h := strings.Repeat("#", min(level, 6)) + " " + title
	if num != "" {
		h = strings.Repeat("#", min(level, 6)) + " " + num + " " + title
	}
	if id != "" {
		h += " {#" + id + "}"
	}
	return h
}

// without returns the nodes that are not elements called one of names, with
// includes resolved.
func (r *renderer) without(kids []*xnode, names ...string) []*xnode {
	var out []*xnode
	for _, k := range r.flat(kids) {
		if k.name == "" || !slices.Contains(names, k.name) {
			out = append(out, k)
		}
	}
	return out
}

// flat replaces include nodes by the content they include, recording the
// included files.
func (r *renderer) flat(kids []*xnode) []*xnode {
	var out []*xnode
	for _, k := range kids {
		if k.name != includeNode {
			out = append(out, k)
			continue
		}
		if r.ch != nil && k.attr["href"] != "" && !slices.Contains(r.ch.Includes, k.attr["href"]) {
			r.ch.Includes = append(r.ch.Includes, k.attr["href"])
		}
		out = append(out, r.flat(k.kids)...)
	}
	return out
}

var (
	blockElems = map[string]bool{
		"para": true, "simpara": true, "formalpara": true, "section": true, "sect1": true, "sect2": true,
		"sect3": true, "sect4": true, "sect5": true, "simplesect": true, "topic": true, "bridgehead": true,
		"procedure": true, "substeps": true, "stepalternatives": true, "itemizedlist": true,
		"orderedlist": true, "variablelist": true, "simplelist": true, "calloutlist": true, "screen": true,
		"programlisting": true, "literallayout": true, "synopsis": true, "note": true, "tip": true,
		"important": true, "warning": true, "caution": true, "table": true, "informaltable": true,
		"example": true, "informalexample": true, "sidebar": true, "figure": true, "informalfigure": true,
		"mediaobject": true, "qandaset": true, "qandadiv": true, "glossary": true, "glossdiv": true,
		"glosslist": true, "glossentry": true, "blockquote": true, "abstract": true, "info": true,
		"title": true, "subtitle": true, "titleabbrev": true, "listitem": true, "step": true,
	}
	inlineElems = map[string]bool{
		"emphasis": true, "phrase": true, "xref": true, "link": true, "quote": true, "citetitle": true,
		"footnote": true, "remark": true, "indexterm": true, "co": true, "anchor": true,
		"inlinemediaobject": true, "uri": true, "replaceable": true, "menuchoice": true, "keycombo": true,
		"productname": true, "trademark": true, "orgname": true, "superscript": true, "subscript": true,
	}
	codeElems = map[string]bool{
		"command": true, "literal": true, "filename": true, "option": true, "varname": true, "envar": true,
		"systemitem": true, "package": true, "code": true, "tag": true, "parameter": true, "prompt": true,
		"userinput": true, "computeroutput": true, "function": true, "classname": true, "constant": true,
		"property": true, "type": true, "markup": true, "token": true, "email": true,
	}
	guiElems = map[string]bool{
		"guimenu": true, "guilabel": true, "guibutton": true, "guisubmenu": true, "guimenuitem": true,
		"guiicon": true, "keycap": true, "accel": true, "mousebutton": true,
	}
	admonitions = map[string]string{"note": "Note", "tip": "Tip", "important": "Important", "warning": "Warning", "caution": "Caution"}
)

// isBlock reports whether n is rendered as block. Unknown elements are blocks
// if they contain blocks.
func isBlock(n *xnode) bool {
	switch {
	case n.name == "":
		return false
	case blockElems[n.name]:
		return true
	case inlineElems[n.name] || codeElems[n.name] || guiElems[n.name]:
		return false
	}
	for _, k := range n.kids {
		if isBlock(k) || (k.name == includeNode && len(k.kids) > 0 && isBlock(k.kids[0])) {
			return true
		}
	}
	return false
}

// blocks renders mixed content: runs of text and inline elements become
// paragraphs, block elements are rendered in between.
func (r *renderer) blocks(kids []*xnode, level, depth int) [][]string {
	var out [][]string
	var buf strings.Builder
	flush := func() {
		if p := paragraph(buf.String()); p != "" {
			out = append(out, []string{p})
		}
		buf.Reset()
	}
	for _, k := range r.flat(kids) {
		if !isBlock(k) {
			appendInline(&buf, r.inline(k))
			continue
		}
		flush()
		out = append(out, r.block(k, level, depth)...)
	}
	flush()
	return out
}

// paragraph collapses the white space of s and escapes characters that would
// start a different block.
func paragraph(s string) string {
	p := strings.Join(strings.Fields(s), " ")
	if strings.HasPrefix(p, "#") || strings.HasPrefix(p, ">") {
		p = `\` + p
	}
	return p
}

func (r *renderer) block(n *xnode, level, depth int) [][]string {
	switch n.name {
	case "info", "title", "subtitle", "titleabbrev":
		return nil
	case "formalpara":
		bs := r.blocks(r.without(n.kids, "title", "info"), level, depth)
		return prefixFirst(bs, "**"+r.title(n)+".**")
	case "section", "sect1", "sect2", "sect3", "sect4", "sect5", "simplesect", "topic":
		title, num := r.title(n), ""
		if n.name != "simplesect" && n.name != "topic" {
			num = r.section(depth)
		}
		r.ch.Sections = append(r.ch.Sections, docparse.Section{ID: n.attr["id"], Number: num, Title: title, Level: depth})
		bs := [][]string{{mdHeading(level, num, title, n.attr["id"])}}
		return append(bs, r.blocks(r.without(n.kids, "info", "title", "subtitle", "titleabbrev", "abstract"), level+1, depth+1)...)
	case "bridgehead":
		return [][]string{{"**" + n.flatText() + "**"}}
	case "procedure":
		steps := r.elems(n.kids, "step")
		title, num := r.title(n), ""
		if title != "" {
			num = r.formal("procedure")
		}
		r.ch.Procedures = append(r.ch.Procedures, docparse.Procedure{ID: n.attr["id"], Number: num, Title: title, Steps: len(steps)})
		bs := r.titled(n, caption("Procedure", num, ""))
		bs = append(bs, r.blocks(r.without(n.kids, "title", "info", "step"), level, depth)...)
		return append(bs, r.list(steps, true, level, depth))
	case "substeps":
		return [][]string{r.list(r.elems(n.kids, "step"), true, level, depth)}
	case "stepalternatives":
		return [][]string{r.list(r.elems(n.kids, "step"), false, level, depth)}
	case "itemizedlist", "orderedlist":
		bs := r.titled(n, "")
		bs = append(bs, r.blocks(r.without(n.kids, "title", "info", "listitem"), level, depth)...)
		return append(bs, r.list(r.elems(n.kids, "listitem"), n.name == "orderedlist", level, depth))
	case "calloutlist":
		return append(r.titled(n, ""), r.calloutList(n, level, depth))
	case "variablelist", "glossary", "glossdiv", "glosslist":
		return append(r.titled(n, ""), r.defList(n, level, depth))
	case "glossentry":
		return [][]string{r.defList(&xnode{kids: []*xnode{n}}, level, depth)}
	case "simplelist":
		return r.simpleList(n)
	case "screen", "programlisting", "literallayout", "synopsis":
		return [][]string{r.codeBlock(n)}
	case "note", "tip", "important", "warning", "caution":
		title := r.title(n)
		r.ch.Admonitions = append(r.ch.Admonitions, docparse.Admonition{Type: n.name, Title: title})
		head := "**" + admonitions[n.name] + ":**"
		if title != "" {
			head += " " + title
		}
		bs := append([][]string{{head}}, r.blocks(r.without(n.kids, "title", "info"), level, depth)...)
		return [][]string{quote(joinBlocks(bs))}
	case "blockquote":
		return [][]string{quote(joinBlocks(r.blocks(n.kids, level, depth)))}
	case "table", "informaltable":
		return append(r.titled(n, r.formalCaption(n, "Table")), r.table(n, level, depth))
	case "example", "informalexample", "sidebar":
		prefix := ""
		if n.name == "example" {
			prefix = r.formalCaption(n, "Example")
		}
		return append(r.titled(n, prefix), r.blocks(r.without(n.kids, "title", "info"), level, depth)...)
	case "figure", "informalfigure":
		r.alt = r.title(n)
		bs := append(r.titled(n, r.formalCaption(n, "Figure")), r.blocks(r.without(n.kids, "title", "info"), level, depth)...)
		r.alt = ""
		return bs
	case "mediaobject":
		if img := r.image(n); img != "" {
			return [][]string{{img}}
		}
		return nil
	case "qandaset", "qandadiv":
		bs := r.titled(n, "")
		for _, k := range r.flat(n.kids) {
			switch k.name {
			case "qandaentry":
				bs = append(bs, prefixFirst(r.blocks(k.child("question").kids, level, depth), "**Q:**")...)
				bs = append(bs, r.blocks(k.child("answer").kids, level, depth)...)
			case "qandadiv":
				bs = append(bs, r.block(k, level, depth)...)
			}
		}
		return bs
	}
	// para, listitem, step, abstract and unknown containers
	return r.blocks(r.without(n.kids, "title", "info"), level, depth)
}

// title returns the title of n as markdown.
func (r *renderer) title(n *xnode) string {
	t := n.titleNode()
	if t == nil {
		return ""
	}
	return strings.TrimSpace(paragraph(r.inlineKids(t)))
}

// formalCaption returns the caption prefix of a table, example or figure n,
// "Table 19.1: " for a numbered table, "Table: " for unnumbered ones. Informal
// objects are not numbered.
func (r *renderer) formalCaption(n *xnode, kind string) string {
	num := ""
	if !strings.HasPrefix(n.name, "informal") && r.title(n) != "" {
		num = r.formal(strings.ToLower(kind))
	}
	return caption(kind, num, ": ")
}

// caption returns "Kind num: ", or unnumbered the fallback.
func caption(kind, num, fallback string) string {
	if num == "" {
		if fallback == "" {
			return ""
		}
		return kind + fallback
	}
	return kind + " " + num + ": "
}

// titled returns the title of n as a bold paragraph, if it has one.
func (r *renderer) titled(n *xnode, prefix string) [][]string {
	if t := r.title(n); t != "" {
		return [][]string{{"**" + prefix + t + "**"}}
	}
	return nil
}

// prefixFirst puts prefix in front of the first paragraph of bs.
func prefixFirst(bs [][]string, prefix string) [][]string {
	if len(bs) > 0 && !isMarkdownBlockStart(bs[0][0]) {
		bs[0] = append([]string{prefix + " " + bs[0][0]}, bs[0][1:]...)
		return bs
	}
	return append([][]string{{prefix}}, bs...)
}

// elems returns the child elements called name, with includes resolved.
func (r *renderer) elems(kids []*xnode, name string) []*xnode {
	var out []*xnode
	for _, k := range r.flat(kids) {
		if k.name == name {
			out = append(out, k)
		}
	}
	return out
}

// list renders items as an ordered or bullet list.
func (r *renderer) list(items []*xnode, ordered bool, level, depth int) []string {
	var bs [][]string
	for i, it := range items {
		marker := "-"
		if ordered {
			marker = strconv.Itoa(i+1) + "."
		}
		bs = append(bs, listItem(marker, r.block(it, level, depth)))
	}
	return joinBlocks(bs)
}

// calloutList renders the callouts as bullet list, each item starting with
// the markers of the <co>s it explains, like they appear in the code block.
func (r *renderer) calloutList(n *xnode, level, depth int) []string {
	var bs [][]string
	for i, c := range r.elems(n.kids, "callout") {
		var marks []string
		for _, id := range strings.Fields(c.attr["arearefs"]) {
			if num, ok := r.callouts[id]; ok {
				marks = append(marks, calloutMark(num))
			}
		}
		if len(marks) == 0 {
			marks = []string{calloutMark(i + 1)}
		}
		bs = append(bs, listItem("-", prefixFirst(r.block(c, level, depth), strings.Join(marks, " "))))
	}
	return joinBlocks(bs)
}

// calloutMark is the marker of callout num in code blocks and callout lists.
// markdown.Analyze strips it from commands.
func calloutMark(num int) string { return "<" + strconv.Itoa(num) + ">" }

// defList renders the varlistentries or glossentries of n as a bullet list
// with the terms in bold.
func (r *renderer) defList(n *xnode, level, depth int) []string {
	var bs [][]string
	for _, e := range r.flat(n.kids) {
		var terms []string
		var body *xnode
		switch e.name {
		case "varlistentry":
			for _, t := range e.children("term") {
				terms = append(terms, strings.TrimSpace(paragraph(r.inlineKids(t))))
			}
			body = e.child("listitem")
		case "glossentry":
			terms = []string{strings.TrimSpace(paragraph(r.inlineKids(e.child("glossterm"))))}
			body = e.child("glossdef")
		default:
			continue
		}
		item := [][]string{{boldOnce(strings.Join(terms, ", "))}}
		if body != nil {
			item = append(item, r.blocks(body.kids, level, depth)...)
		}
		bs = append(bs, listItem("-", item))
	}
	return joinBlocks(bs)
}

func (r *renderer) simpleList(n *xnode) [][]string {
	var members []string
	for _, m := range r.elems(n.kids, "member") {
		members = append(members, strings.TrimSpace(paragraph(r.inlineKids(m))))
	}
	if n.attr["type"] == "inline" {
		return [][]string{{strings.Join(members, ", ")}}
	}
	var lines []string
	for _, m := range members {
		lines = append(lines, "- "+m)
	}
	return [][]string{lines}
}

// listItem puts marker in front of the first line and indents the rest by
// four spaces, which gomarkdown needs to keep fenced code inside the item.
// A fence or quote may follow the marker on the same line.
func listItem(marker string, bs [][]string) []string {
	lines := joinBlocks(bs)
	if len(lines) == 0 {
		return []string{marker}
	}
	out := indent(lines, "    ")
	out[0] = strings.TrimRight(marker+" "+lines[0], " ")
	return out
}

var (
	reOrderedItem = regexp.MustCompile(`^\d+[.)](?:\s|$)`)
	reFence       = regexp.MustCompile("^\\s*(```|~~~)")
)

// isMarkdownBlockStart reports whether ln starts something other than a
// paragraph, which must not follow a list marker on the same line.
func isMarkdownBlockStart(ln string) bool {
	return reFence.MatchString(ln) || strings.HasPrefix(ln, ">") || strings.HasPrefix(ln, "|") ||
		strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "- ") || reOrderedItem.MatchString(ln)
}

// indent prefixes the lines. Blank lines stay blank, except inside fenced
// code where they must stay part of the block.
func indent(lines []string, prefix string) []string {
	out := make([]string, len(lines))
	fenced := false
	for i, ln := range lines {
		if reFence.MatchString(ln) {
			fenced = !fenced
		}
		if ln == "" && !fenced {
			out[i] = strings.TrimRight(prefix, " ")
		} else {
			out[i] = prefix + ln
		}
	}
	return out
}

func quote(lines []string) []string {
	return indent(lines, "> ")
}

// joinBlocks joins blocks with blank lines, skipping empty ones.
func joinBlocks(bs [][]string) []string {
	var out []string
	for _, b := range bs {
		if len(b) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, b...)
	}
	return out
}

// codeBlock renders a screen or programlisting as fenced code block. The
// language attribute becomes the info string; screens with a prompt are
// marked as console sessions.
func (r *renderer) codeBlock(n *xnode) []string {
	text := strings.ReplaceAll(strings.TrimRight(r.verbatim(n), " \t\r\n"), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	for len(lines) > 1 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	info := n.attr["language"]
	if info == "" && len(n.find("prompt")) > 0 {
		info = "console"
	}
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	lines = append([]string{fence + info}, lines...)
	return append(lines, fence)
}

// verbatim returns the text of a screen or programlisting with its callouts
// as markers (" <1>", numbered within n) and xrefs to them as their marker.
// The numbers are recorded for the calloutlist and xrefs that follow.
func (r *renderer) verbatim(n *xnode) string {
	for i, co := range n.find("co") {
		if id := co.attr["id"]; id != "" {
			r.callouts[id] = i + 1
		}
	}
	var b strings.Builder
	mark := func(num int) {
		if s := b.String(); s != "" && !strings.HasSuffix(s, " ") && !strings.HasSuffix(s, "\n") {
			b.WriteByte(' ')
		}
		b.WriteString(calloutMark(num))
	}
	cos := 0
	var walk func(x *xnode)
	walk = func(x *xnode) {
		switch x.name {
		case "":
			b.WriteString(x.text)
		case "remark":
		case "co":
			cos++
			mark(cos)
		case "xref":
			if num, ok := r.callouts[x.attr["linkend"]]; ok {
				mark(num)
			}
		default:
			for _, k := range x.kids {
				walk(k)
			}
		}
	}
	walk(n)
	return b.String()
}

// table renders a CALS or HTML table as GFM table. Block content of cells is
// flattened, rows without header use the first row as header. A cell
// spanning rows (morerows, rowspan) is repeated in each of them, one spanning
// columns (namest/nameend, colspan) fills the first and leaves the others
// empty, so that every cell stays in its column.
func (r *renderer) table(n *xnode, level, depth int) []string {
	var head []string
	var rows [][]string
	cols := 0
	var walk func(x *xnode, inHead bool, colIdx map[string]int)
	walk = func(x *xnode, inHead bool, colIdx map[string]int) {
		carried := map[int]*carry{} // cells of rows above spanning into the next rows, by column
		for _, k := range r.flat(x.kids) {
			switch k.name {
			case "tgroup":
				cols = max(cols, atoi(k.attr["cols"]))
				walk(k, false, colSpecs(r.flat(k.kids)))
			case "tbody", "tfoot":
				walk(k, false, colIdx)
			case "thead":
				walk(k, true, colIdx)
			case "row", "tr":
				cells := r.row(k, colIdx, carried, level, depth)
				if inHead && head == nil {
					head = cells
				} else {
					rows = append(rows, cells)
				}
			}
		}
	}
	walk(n, false, nil)
	if head == nil && len(rows) > 0 {
		head, rows = rows[0], rows[1:]
	}
	cols = max(cols, len(head))
	for _, row := range rows {
		cols = max(cols, len(row))
	}
	if cols == 0 {
		return nil
	}
	sep := make([]string, cols)
	for i := range sep {
		sep[i] = "---"
	}
	lines := []string{tableRow(head, cols), tableRow(sep, cols)}
	for _, row := range rows {
		lines = append(lines, tableRow(row, cols))
	}
	return lines
}

// carry is a cell spanning into the rows below.
type carry struct {
	text string
	rows int // rows left to fill
}

// colSpecs maps the colnames of the colspecs to column indexes.
func colSpecs(kids []*xnode) map[string]int {
	idx := map[string]int{}
	col := 0
	for _, k := range kids {
		if k.name != "colspec" {
			continue
		}
		if n := atoi(k.attr["colnum"]); n > 0 {
			col = n - 1
		}
		if name := k.attr["colname"]; name != "" {
			idx[name] = col
		}
		col++
	}
	return idx
}

// row renders the cells of a table row, placing them in their columns: cells
// carried from rows above take their columns first, namest and colname name
// the column of a CALS entry.
func (r *renderer) row(row *xnode, colIdx map[string]int, carried map[int]*carry, level, depth int) []string {
	var cells []string
	taken := map[int]bool{}
	set := func(i int, s string) {
		for len(cells) <= i {
			cells = append(cells, "")
		}
		cells[i] = s
		taken[i] = true
	}
	for i, c := range carried {
		set(i, c.text)
		if c.rows--; c.rows == 0 {
			delete(carried, i)
		}
	}
	col := 0
	for _, c := range r.flat(row.kids) {
		if c.name != "entry" && c.name != "td" && c.name != "th" {
			continue
		}
		if i, ok := colIdx[firstNonEmpty(c.attr["namest"], c.attr["colname"])]; ok {
			col = i
		}
		for taken[col] {
			col++
		}
		end := col
		if i, ok := colIdx[c.attr["nameend"]]; ok && i > col {
			end = i
		}
		if span := atoi(c.attr["colspan"]); span > 1 {
			end = col + span - 1
		}
		text := r.cell(c, level, depth)
		rows := max(atoi(c.attr["morerows"]), atoi(c.attr["rowspan"])-1)
		for i := col; i <= end; i++ {
			s := ""
			if i == col {
				s = text
			}
			set(i, s)
			if rows > 0 {
				carried[i] = &carry{text: s, rows: rows}
			}
		}
		col = end + 1
	}
	return cells
}

// atoi returns the number in s, 0 if it is none.
func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func tableRow(cells []string, cols int) string {
	for len(cells) < cols {
		cells = append(cells, "")
	}
	return "| " + strings.Join(cells, " | ") + " |"
}

// cell renders a table cell on one line: code blocks become inline code and
// blocks are separated by <br>.
func (r *renderer) cell(c *xnode, level, depth int) string {
	var parts []string
	for _, b := range r.blocks(c.kids, level, depth) {
		if reFence.MatchString(b[0]) && len(b) >= 2 {
			parts = append(parts, inlineCode(strings.Join(b[1:len(b)-1], " ")))
		} else {
			parts = append(parts, strings.Join(strings.Fields(strings.Join(b, " ")), " "))
		}
	}
	return strings.ReplaceAll(strings.Join(parts, "<br>"), "|", `\|`)
}

// image renders the first image of a mediaobject.
func (r *renderer) image(n *xnode) string {
	file := ""
	for _, d := range n.find("imagedata") {
		if file = d.attr["fileref"]; file != "" {
			break
		}
	}
	if file == "" {
		return ""
	}
	if !strings.Contains(file, "/") {
		file = path.Join(r.imgDir, file)
	}
	alt := firstNonEmpty(n.child("textobject").flatText(), n.child("alt").flatText(), r.alt)
	if r.ch != nil {
		r.ch.Images = append(r.ch.Images, docparse.Image{File: file, Alt: alt})
	}
	return "![" + strings.ReplaceAll(alt, "]", `\]`) + "](" + file + ")"
}

// inlineKids renders the children of n as inline markdown.
func (r *renderer) inlineKids(n *xnode) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	for _, k := range r.flat(n.kids) {
		appendInline(&b, r.inline(k))
	}
	return b.String()
}

// appendInline appends s to b, separating adjacent bold spans ("**A** **B**"
// instead of "**A****B**").
func appendInline(b *strings.Builder, s string) {
	if strings.HasPrefix(s, "**") && strings.HasSuffix(b.String(), "**") {
		b.WriteByte(' ')
	}
	b.WriteString(s)
}

// keyNames are the names of keys given by <keycap function="..."/>.
var keyNames = map[string]string{
	"control": "Ctrl", "alt": "Alt", "shift": "Shift", "meta": "Meta", "command": "Cmd", "option": "Option",
	"enter": "Enter", "escape": "Esc", "tab": "Tab", "backspace": "Backspace", "delete": "Del",
	"space": "Space", "home": "Home", "end": "End", "pageup": "Page Up", "pagedown": "Page Down",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
}

// keycap returns the text of a keycap, or the name of its function key.
func (r *renderer) keycap(n *xnode) string {
	if t := strings.Join(strings.Fields(r.inlineKids(n)), " "); t != "" {
		return t
	}
	return firstNonEmpty(keyNames[n.attr["function"]], n.attr["function"])
}

func (r *renderer) inline(n *xnode) string {
	if n.name == "" {
		return n.text
	}
	switch {
	case codeElems[n.name]:
		return wrap(n.rawText(), inlineCode)
	case n.name == "keycap":
		return bold(r.keycap(n))
	case guiElems[n.name]:
		return wrap(r.inlineKids(n), bold)
	}
	switch n.name {
	case "remark", "indexterm", "co", "anchor", "info":
		return ""
	case "emphasis":
		if role := n.attr["role"]; role == "bold" || role == "strong" {
			return wrap(r.inlineKids(n), bold)
		}
		return wrap(r.inlineKids(n), italic)
	case "replaceable", "citetitle":
		return wrap(r.inlineKids(n), italic)
	case "quote":
		return "“" + r.inlineKids(n) + "”"
	case "menuchoice", "keycombo":
		sep := " > "
		if n.name == "keycombo" {
			sep = "+"
		}
		var parts []string
		for _, k := range r.flat(n.kids) {
			switch k.name {
			case "", "shortcut":
			case "keycap":
				parts = append(parts, r.keycap(k))
			default:
				parts = append(parts, strings.Join(strings.Fields(r.inlineKids(k)), " "))
			}
		}
		return bold(strings.Join(parts, sep))
	case "xref":
		if num, ok := r.callouts[n.attr["linkend"]]; ok {
			return calloutMark(num)
		}
		return r.xref(n.attr["linkend"], "")
	case "link":
		return r.link(n)
	case "footnote":
		return " (" + r.text(n) + ")"
	case "inlinemediaobject":
		return r.image(n)
	}
	return r.inlineKids(n)
}

func (r *renderer) xref(linkend, text string) string {
	title, ok := r.ids[linkend]
	if r.ch != nil && !containsXref(r.ch.Xrefs, linkend) {
		r.ch.Xrefs = append(r.ch.Xrefs, docparse.Xref{Linkend: linkend, Resolved: ok})
	}
	text = firstNonEmpty(text, title, linkend)
	return "[" + text + "](#" + linkend + ")"
}

func containsXref(xs []docparse.Xref, linkend string) bool {
	for _, x := range xs {
		if x.Linkend == linkend {
			return true
		}
	}
	return false
}

func (r *renderer) link(n *xnode) string {
	text := strings.Join(strings.Fields(r.inlineKids(n)), " ")
	url := n.attr["href"]
	if url == "" {
		if n.attr["linkend"] != "" {
			return r.xref(n.attr["linkend"], text)
		}
		return text
	}
	if r.ch != nil {
		found := false
		for _, l := range r.ch.Links {
			found = found || l.URL == url
		}
		if !found {
			r.ch.Links = append(r.ch.Links, docparse.Link{URL: url, Text: text})
		}
	}
	if text == "" {
		return "<" + url + ">"
	}
	return "[" + text + "](" + url + ")"
}

// wrap applies style to the trimmed s, keeping the surrounding white space
// outside of the markup.
func wrap(s string, style func(string) string) string {
	t := strings.Join(strings.Fields(s), " ")
	if t == "" {
		return s
	}
	lead, trail := "", ""
	if s[0] == ' ' || s[0] == '\n' || s[0] == '\t' {
		lead = " "
	}
	if c := s[len(s)-1]; c == ' ' || c == '\n' || c == '\t' {
		trail = " "
	}
	return lead + style(t) + trail
}

func bold(s string) string { return "**" + s + "**" }

// boldOnce makes s bold unless it already is, as terms are often GUI labels.
func boldOnce(s string) string {
	if len(s) > 4 && strings.HasPrefix(s, "**") && strings.HasSuffix(s, "**") && !strings.Contains(s[2:len(s)-2], "**") {
		return s
	}
	return bold(s)
}
func italic(s string) string { return "*" + s + "*" }

func inlineCode(s string) string {
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}

// text returns the plain text of n with xrefs replaced by their target's
// title, for summaries and glossary definitions.
func (r *renderer) text(n *xnode) string {
	var b strings.Builder
	var walk func(x *xnode)
	walk = func(x *xnode) {
		switch {
		case x.name == "":
			b.WriteString(x.text)
			return
		case x.name == "remark" || x.name == "co" || x.name == "indexterm":
			return
		case x.name == "xref":
			b.WriteString(firstNonEmpty(r.ids[x.attr["linkend"]], x.attr["linkend"]))
			return
		case blockElems[x.name]:
			b.WriteString(" ")
			defer b.WriteString(" ")
		}
		for _, k := range x.kids {
			walk(k)
		}
	}
	if n != nil {
		walk(n)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

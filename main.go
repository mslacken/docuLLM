package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
	"gopkg.in/yaml.v3"
)

type node struct {
	Type     string
	Content  map[string]any
	Children []node
}

func main() {
	file := flag.String("f", "", "markdown file to parse (default: stdin)")
	flag.Parse()

	var data []byte
	var err error
	if *file != "" {
		data, err = os.ReadFile(*file)
	} else {
		data, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error reading input:", err)
		os.Exit(1)
	}

	meta, body := splitFrontMatter(data)

	p := parser.NewWithExtensions(parser.CommonExtensions)
	doc := p.Parse(body)

	out := build(doc)
	out = transform(out)
	if meta != nil {
		out.Children = append([]node{metaNode(meta)}, out.Children...)
	}
	out.Children = collapseGlossary(out.Children)

	enc, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error encoding json:", err)
		os.Exit(1)
	}
	fmt.Println(string(enc))
}

var frontMatter = regexp.MustCompile(`(?s)\A---\n(.*?\n)---\n`)

func splitFrontMatter(data []byte) (map[string]any, []byte) {
	m := frontMatter.FindSubmatch(data)
	if m == nil {
		return nil, data
	}
	var meta map[string]any
	if err := yaml.Unmarshal(m[1], &meta); err != nil {
		return nil, data
	}
	full := frontMatter.Find(data)
	return meta, data[len(full):]
}

func metaNode(meta map[string]any) node {
	content := make(map[string]any, len(meta))
	for k, v := range meta {
		content[k] = v
	}
	return node{Type: "FrontMatter", Content: content, Children: []node{}}
}

func build(n ast.Node) node {
	out := node{Type: typeName(n), Content: content(n)}
	for child := ast.GetFirstChild(n); child != nil; child = ast.GetNextNode(child) {
		out.Children = append(out.Children, build(child))
	}
	if out.Children == nil {
		out.Children = []node{}
	}
	return out
}

var glossaryHeading = regexp.MustCompile(`(?i)glossary`)

func collapseGlossary(children []node) []node {
	var out []node
	glossary := false
	for i := 0; i < len(children); i++ {
		c := children[i]
		if c.Type == "Heading" {
			glossary = glossaryHeading.MatchString(textOf(c, "text"))
			out = append(out, c)
			continue
		}
		if !glossary || c.Type != "Paragraph" || !isTerm(c) {
			out = append(out, c)
			continue
		}
		j := i + 1
		defs := []string{}
		for j < len(children) && children[j].Type == "Paragraph" && !isTerm(children[j]) {
			defs = append(defs, textOf(children[j], "text"))
			j++
		}
		if len(defs) == 0 {
			out = append(out, c)
			continue
		}
		out = append(out, node{
			Type: "GlossaryEntry",
			Content: map[string]any{
				"term":       textOf(c, "text"),
				"definition": strings.Join(defs, " "),
			},
			Children: []node{},
		})
		i = j - 1
	}
	return out
}

func textOf(n node, key string) string {
	if n.Content == nil {
		return ""
	}
	if s, ok := n.Content[key].(string); ok {
		return s
	}
	return ""
}

func wordCount(s string) int { return len(strings.Fields(strings.TrimSpace(s))) }

func isTerm(node node) bool {
	t := textOf(node, "text")
	if t == "" || wordCount(t) > 8 || utf8.RuneCountInString(t) > 50 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(strings.TrimSpace(t))
	return unicode.IsLetter(r)
}

func typeName(n ast.Node) string {
	if n == nil {
		return "nil"
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", n), "*ast.")
}

func content(n ast.Node) map[string]any {
	m := map[string]any{}
	switch v := n.(type) {
	case *ast.Heading:
		if v.Level > 0 {
			m["level"] = v.Level
		}
		if v.HeadingID != "" {
			m["id"] = v.HeadingID
		}
		m["text"] = innerText(n)
	case *ast.Paragraph:
		m["text"] = innerText(n)
	case *ast.Code:
		m["text"] = string(v.Literal)
	case *ast.CodeBlock:
		m["info"] = string(v.Info)
		m["text"] = string(v.Literal)
	case *ast.Link:
		m["destination"] = string(v.Destination)
		if v.Title != nil {
			m["title"] = string(v.Title)
		}
		m["text"] = innerText(n)
	case *ast.Image:
		m["destination"] = string(v.Destination)
		if v.Title != nil {
			m["alt"] = string(v.Title)
		}
	case *ast.List:
		if v.ListFlags&ast.ListTypeOrdered != 0 {
			m["ordered"] = true
			if v.Start > 0 {
				m["start"] = v.Start
			}
		}
		m["tight"] = v.Tight
	case *ast.ListItem:
		m["tight"] = v.Tight
	case *ast.HTMLBlock:
		m["text"] = string(v.Literal)
	case *ast.HTMLSpan:
		m["text"] = string(v.Literal)
	case *ast.Text:
		m["text"] = string(v.Literal)
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// transform performs post-parse cleanup so the JSON tree matches the intent of
// the (HTML->markdown) converter: raw `<img>` paragraphs become Image nodes,
// the split-out list markers ("-", "1.", ...) are folded back into real lists,
// and code blocks get a best-effort language tag so shell console sessions and
// standalone commands are distinguishable.
func transform(n node) node {
	for i := range n.Children {
		n.Children[i] = transform(n.Children[i])
	}
	for i := range n.Children {
		c := n.Children[i]
		if c.Type == "Paragraph" {
			if img := imageFrom(c); img.Type == "Image" {
				n.Children[i] = img
			}
		}
		if c.Type == "CodeBlock" {
			if c.Content == nil {
				c.Content = map[string]any{}
			}
			c.Content["language"] = detectLang(c)
		}
	}
	n.Children = groupLists(n.Children)
	return n
}

var htmlAttrREs = map[string]*regexp.Regexp{
	"alt":   regexp.MustCompile(`(?i)\balt\s*=\s*"([^"]*)"`),
	"src":   regexp.MustCompile(`(?i)\bsrc\s*=\s*"([^"]*)"`),
	"title": regexp.MustCompile(`(?i)\btitle\s*=\s*"([^"]*)"  `),
}

func htmlAttr(tag, name string) string {
	re, ok := htmlAttrREs[name]
	if !ok {
		return ""
	}
	m := re.FindStringSubmatch(tag)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func imgTagIn(n node) string {
	if n.Type == "HTMLSpan" {
		if s := textOf(n, "text"); strings.Contains(s, "<img") {
			return s
		}
	}
	for _, c := range n.Children {
		if s := imgTagIn(c); s != "" {
			return s
		}
	}
	return ""
}

func nInnerText(n node) string {
	var t string
	var walk func(nn node)
	walk = func(nn node) {
		if nn.Type == "Text" || nn.Type == "Code" {
			if s, ok := nn.Content["text"].(string); ok {
				t += s
			}
		}
		for _, c := range nn.Children {
			walk(c)
		}
	}
	walk(n)
	return t
}

func imageFrom(p node) node {
	if p.Type != "Paragraph" {
		return node{}
	}
	if strings.TrimSpace(nInnerText(p)) != "" {
		return node{}
	}
	tag := imgTagIn(p)
	if tag == "" {
		return node{}
	}
	c := map[string]any{}
	if v := htmlAttr(tag, "src"); v != "" {
		c["src"] = v
	}
	if v := htmlAttr(tag, "alt"); v != "" {
		c["alt"] = v
	}
	if v := htmlAttr(tag, "title"); v != "" {
		c["title"] = v
	}
	return node{Type: "Image", Content: c, Children: []node{}}
}

var (
	reBullet  = regexp.MustCompile(`^[-*+]$`)
	reOrdered = regexp.MustCompile(`^\d{1,3}[.)]$`)

	reShellPrompt = regexp.MustCompile(`^(?:> \S|crm\(|\S*@\S*\s*[>#]\s|\S+\s*[>#]\s)`)
	reShellCmd    = regexp.MustCompile(`^\s*(?:[#$%>]\s+)*(?:sudo\s+|crm\s+|crmsh\s+|systemctl\s+|sbd\s+|fence_[a-z]+\s+|corosync\s+|ha\s+|zypper\s+|rpm\s+|crmd\s+|crm_[a-z]+\s+|pacemaker[a-z]+\s+|crash_test\s+)`)
)

// markerKind reports 0 (not a bare list marker), 1 (bullet) or 2 (ordered) for a
// paragraph whose entire text is a bare marker like "-", "1." or "2)".
func markerKind(c node) int {
	if c.Type != "Paragraph" {
		return 0
	}
	t := strings.TrimSpace(textOf(c, "text"))
	switch {
	case reBullet.MatchString(t):
		return 1
	case reOrdered.MatchString(t):
		return 2
	}
	return 0
}

// groupLists folds the converter's "marker paragraph + content paragraph" runs
// back into List/ListItem nodes. A marker starts an item; the item body is the
// immediately following paragraph plus any directly attached CodeBlocks (the
// command that step runs). The list ends at the first element that is neither a
// marker nor an item body.
func groupLists(children []node) []node {
	var out []node
	i := 0
	for i < len(children) {
		mk := markerKind(children[i])
		if mk == 0 || i+1 >= len(children) || children[i+1].Type != "Paragraph" || markerKind(children[i+1]) != 0 {
			out = append(out, children[i])
			i++
			continue
		}
		ordered := mk == 2
		var items [][]node
		j := i
		for j < len(children) {
			if markerKind(children[j]) == 0 {
				break
			}
			j++
			if j >= len(children) || children[j].Type != "Paragraph" || markerKind(children[j]) != 0 {
				break
			}
			item := []node{children[j]}
			k := j + 1
			for k < len(children) && children[k].Type == "CodeBlock" {
				item = append(item, children[k])
				k++
			}
			items = append(items, item)
			j = k
		}
		if len(items) == 0 {
			out = append(out, children[i])
			i++
			continue
		}
		c := map[string]any{}
		if ordered {
			c["ordered"] = true
		}
		var lis []node
		for _, it := range items {
			lis = append(lis, node{Type: "ListItem", Children: it})
		}
		out = append(out, node{Type: "List", Content: c, Children: lis})
		i = j
	}
	if out == nil {
		out = []node{}
	}
	return out
}

func detectLang(n node) string {
	s, _ := n.Content["text"].(string)
	console, shell := false, false
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if reShellPrompt.MatchString(t) {
			console = true
		} else if reShellCmd.MatchString(t) {
			shell = true
		}
	}
	switch {
	case console:
		return "console"
	case shell:
		return "shell"
	}
	return ""
}

func innerText(n ast.Node) string {
	text := ""
	ast.WalkFunc(n, func(node ast.Node, entering bool) ast.WalkStatus {
		switch t := node.(type) {
		case *ast.Text:
			text += string(t.Literal)
		case *ast.Code:
			text += string(t.Literal)
		}
		return ast.GoToNext
	})
	return text
}

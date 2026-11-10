package markdown

import (
	"regexp"
	"strings"

	"github.com/gomarkdown/markdown/parser"
)

// Analysis is what is extracted from the markdown of a chapter.
type Analysis struct {
	Commands []string // shell commands of the code blocks, without prompt
	Lead     string   // paragraphs between the chapter heading and the first sub heading
	Prose    string   // running text without code, for glossary matching
}

// Analyze parses the markdown lines of a chapter, starting with its heading.
func Analyze(lines []string) Analysis {
	children := parseNodes(lines)
	return Analysis{Commands: commandsIn(children), Lead: leadText(children), Prose: proseOf(children)}
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

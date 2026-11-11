// Package text parses plain text files. Every file becomes one Document; its
// chapters start at headings underlined with "===" or "---" (as in setext
// markdown and many README files), or the whole file is one chapter.
package text

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"docullm/internal/docparse"
	"docullm/internal/docparse/markdown"
)

var reUnderline = regexp.MustCompile(`^(?:={3,}|-{3,})\s*$`)

// ReadFile turns the text file p into a part holding one document and the
// lines of the file.
func ReadFile(p string) (docparse.Part, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return docparse.Part{}, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	doc := docparse.Document{ID: docparse.Slug(base), Title: firstLine(lines), Chapters: []docparse.Chapter{}}
	if doc.Title == "" {
		doc.Title = base
	}

	// a chapter starts at a heading, the text before the first one is a
	// chapter of its own if it is not empty
	var starts []int
	for i := 0; i+1 < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" && reUnderline.MatchString(lines[i+1]) {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 || strings.TrimSpace(strings.Join(lines[:starts[0]], "")) != "" {
		starts = append([]int{0}, starts...)
	}
	ids := map[string]int{}
	for n, start := range starts {
		end := len(lines)
		if n+1 < len(starts) {
			end = starts[n+1]
		}
		for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		body, title := start, doc.Title
		if start+1 < len(lines) && reUnderline.MatchString(lines[start+1]) {
			body, title = start+2, strings.TrimSpace(lines[start])
		}
		text := strings.Join(lines[start:end], "\n")
		// commands are usually indented in plain text
		var trimmed []string
		for _, ln := range lines[start:end] {
			trimmed = append(trimmed, strings.TrimSpace(ln))
		}
		doc.Chapters = append(doc.Chapters, docparse.Chapter{
			ID:       docparse.UniqueID(ids, docparse.Slug(title)),
			Title:    title,
			Summary:  leadParagraph(lines[min(body, end):end]),
			Start:    uint(start + 1),
			End:      uint(end),
			FilePath: p,
			Commands: markdown.CommandsInText(strings.Join(trimmed, "\n")),
			LinesKey: p,
			Prose:    text,
		})
	}
	if len(doc.Chapters) > 0 {
		doc.Summary = doc.Chapters[0].Summary
	}
	return docparse.Part{Documents: []docparse.Document{doc}, Lines: map[string][]string{p: lines}}, nil
}

// firstLine returns the first non-blank line, trimmed.
func firstLine(lines []string) string {
	for _, ln := range lines {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return ""
}

// leadParagraph returns the first paragraph of lines, joined into one line.
func leadParagraph(lines []string) string {
	var parts []string
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			if len(parts) > 0 {
				break
			}
			continue
		}
		parts = append(parts, t)
	}
	return strings.Join(parts, " ")
}

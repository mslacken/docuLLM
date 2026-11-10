// Package loader reads a corpus with the parser matching its input: DocBook
// assemblies published through DC files, or else markdown files.
package loader

import (
	"docullm/internal/docparse"
	"docullm/internal/docparse/docbook"
	"docullm/internal/docparse/markdown"
)

// Load reads a single DC file or a directory with DC files at its top level
// as DocBook assemblies. Anything else is read as markdown: a single
// (possibly combined) file or a directory tree of markdown files.
func Load(root string) (*docparse.Corpus, error) {
	if docbook.Detect(root) {
		return docbook.Load(root)
	}
	return markdown.Load(root)
}

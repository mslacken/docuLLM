// Package loader reads a corpus from a list of files, each with the parser
// matching its type: DocBook assemblies published through DC files, markdown
// files and plain text files.
package loader

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"docullm/internal/docparse"
	"docullm/internal/docparse/docbook"
	"docullm/internal/docparse/markdown"
	"docullm/internal/docparse/text"
)

// DocType is the format of an input file.
type DocType int

const (
	DocBook  DocType = iota // DC file of a DocBook assembly
	Markdown                // markdown file
	Text                    // plain text file
)

// DocFile is an input file. Root is the directory the file was found in, or
// "" if it was named directly. DocBook files are resolved relative to their
// own directory, which all DC files in it share.
type DocFile struct {
	Path string
	Root string
	Type DocType
}

// TypeOf returns the type of the file p by its name: DC-* files are DocBook,
// *.md markdown and *.txt plain text. ok is false for anything else.
func TypeOf(p string) (t DocType, ok bool) {
	switch ext := strings.ToLower(filepath.Ext(p)); {
	case docbook.IsDCFile(p):
		return DocBook, true
	case ext == ".md" || ext == ".markdown":
		return Markdown, true
	case ext == ".txt" || ext == ".text":
		return Text, true
	}
	return 0, false
}

// File returns the input file p, named directly. Files TypeOf does not
// recognize are read as markdown.
func File(p string) DocFile {
	t, ok := TypeOf(p)
	if !ok {
		t = Markdown
	}
	return DocFile{Path: p, Type: t}
}

// Find returns the files below dir that TypeOf recognizes. A directory with
// DC files at its top level is a DocBook corpus: only its DC files are taken
// and it is not descended into, as its topics are read through the DC files
// and its l10n/ copies are generated. Hidden directories are skipped. If dir
// is a file, it is returned as File does.
func Find(dir string) ([]DocFile, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return []DocFile{File(dir)}, nil
	}
	var out []DocFile
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if dcs := docbook.DCFiles(p); len(dcs) > 0 {
				for _, name := range dcs {
					out = append(out, DocFile{Path: filepath.Join(p, name), Root: dir, Type: DocBook})
				}
				return filepath.SkipDir
			}
			return nil
		}
		if t, ok := TypeOf(p); ok && t != DocBook {
			out = append(out, DocFile{Path: p, Root: dir, Type: t})
		}
		return nil
	})
	return out, err
}

// result is what was read from one file.
type result struct {
	part docparse.Part
	segs []markdown.Segment // markdown files are grouped into documents later
	err  error
}

// Load reads files in parallel and builds the corpus from them. DocBook
// files in the same directory share one docbook.Loader, so that common topics
// and entity files are parsed once. The corpus is then filled in file order,
// so that the document order and the first definition winning for a glossary
// term stay deterministic, and finally its indexes are built. Unreadable files
// are skipped with a warning; it is an error if no document is left.
func Load(files []DocFile) (*docparse.Corpus, error) {
	xml := map[string]*docbook.Loader{}
	for _, f := range files {
		if dir := filepath.Dir(f.Path); f.Type == DocBook && xml[dir] == nil {
			xml[dir] = docbook.NewLoader(dir)
		}
	}

	results := make([]result, len(files))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = read(files[i], xml)
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	var segs []markdown.Segment
	for i, r := range results {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %v\n", files[i].Path, r.err)
			continue
		}
		segs = append(segs, r.segs...)
	}
	c := docparse.NewCorpus()
	mdAdded := false
	for i, r := range results {
		if r.err != nil {
			continue
		}
		// the markdown documents go where the first markdown file is
		if files[i].Type == Markdown && !mdAdded {
			c.Add(markdown.Build(segs))
			mdAdded = true
		}
		c.Add(r.part)
	}
	if len(c.Documents) == 0 {
		return nil, fmt.Errorf("no usable documents found")
	}
	c.Finish()
	return c, nil
}

// read reads the file f with the parser for its type.
func read(f DocFile, xml map[string]*docbook.Loader) result {
	switch f.Type {
	case DocBook:
		p, err := xml[filepath.Dir(f.Path)].Read(filepath.Base(f.Path))
		return result{part: p, err: err}
	case Markdown:
		segs, err := markdown.ReadFile(f.Path, f.Root)
		return result{segs: segs, err: err}
	case Text:
		p, err := text.ReadFile(f.Path)
		return result{part: p, err: err}
	}
	return result{err: fmt.Errorf("unknown document type %d", f.Type)}
}

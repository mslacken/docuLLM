# Present openSUSE/SUSE documentation to an LLM

Having an MCP server accessing the openSUSE/SUSE documentation improves the
quality of answers to related questions about this distribution a lot.
This project delivers a server that can read
* docbook
* asciidoc
* markdown
* text
and presents every document following Go structs (not fully specified yet):
```
type Document struct {
	ID       string    `json:"id" yaml:"id"`
	Title    string    `json:"title" yaml:"title"`
	Summary  string    `json:"summary" yaml:"summary"`
	Distro   Distro    `json:"distro" yaml:"distro"`
	Product  Product   `json:"product" yaml:"product"`
	Chapters []Chapter `json:"chapters" yaml:"chapters"`
	License  string    `json:"license,omitempty" yaml:"license,omitempty"`
}

type Chapter struct {
	ID         string   `json:"id" yaml:"id"`
	Title      string   `json:"chapter_title" yaml:"chapter_title"`
	Summary    string   `json:"summary,omitempty" yaml:"summary,omitempty"`         // lead paragraphs
	Start      uint     `json:"start" yaml:"start"`                                 // first line, counted in the original source file
	End        uint     `json:"end" yaml:"end"`                                     // last line, counted in the original source file
	FilePath   string   `json:"file_path" yaml:"file_path"`                         // file the chapter was parsed from (preferred)
	SourcePath string   `json:"source_path,omitempty" yaml:"source_path,omitempty"` // original file before combining
	Commands   []string `json:"commands,omitempty" yaml:"commands,omitempty"`
	Terms      []string `json:"terms,omitempty" yaml:"terms,omitempty"` // glossary terms mentioned in the prose

}
```
which are defined in [./internal/docparse/document.go](./internal/docparse/document.go)

The MCP server offers the following functions:
* list_documents, which lists all documents in the corpus
* read_document, for direct access to a document or its chapters; also includes the used shell commands and terms
* find_command, a backward search for where a shell command is mentioned in the documentation
* define_term, which returns the definition of a term
These functions are also offered as a resource, so that e.g. "docs://{doc_id}/{chapter_id}" will allow access to the requested document.

There is also a web interface which just wraps the results of the MCP functions in a template.

## Usage

Check out this repository and run:
```
go run ./cmd/docullm/ -web -debug -d $PATH_TO_SUSE_DOC
```

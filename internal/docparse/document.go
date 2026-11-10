package docparse

import (
	"fmt"
	"regexp"
	"strings"
)

// Document is a parsed guide.
type Document struct {
	ID       string    `json:"id" yaml:"id"`
	Title    string    `json:"title" yaml:"title"`
	Summary  string    `json:"summary" yaml:"summary"`
	Distro   Distro    `json:"distro" yaml:"distro"`
	Product  Product   `json:"product" yaml:"product"`
	Chapters []Chapter `json:"chapters" yaml:"chapters"`
	License  string    `json:"license,omitempty" yaml:"license,omitempty"`

	// DocBook assemblies only; not exposed via MCP
	Source      *DocSource  `json:"source,omitempty" yaml:"source,omitempty"`
	Profile     *Profile    `json:"profile,omitempty" yaml:"profile,omitempty"`
	Subtitle    string      `json:"subtitle,omitempty" yaml:"subtitle,omitempty"`
	Meta        *DocMeta    `json:"meta,omitempty" yaml:"meta,omitempty"`
	Revisions   []Revision  `json:"revisions,omitempty" yaml:"revisions,omitempty"`
	Bugtracker  *Bugtracker `json:"bugtracker,omitempty" yaml:"bugtracker,omitempty"`
	Translation string      `json:"translation,omitempty" yaml:"translation,omitempty"` // "yes" or "no"
}

// Distro is the distribution a document applies to, e.g. "SUSE Linux
// Enterprise Server" and its versions (16.0, 16.1, ...).
type Distro struct {
	Name     string   `json:"name,omitempty" yaml:"name,omitempty"`
	Versions []string `json:"versions,omitempty" yaml:"versions,omitempty"`
}

// Product is a specific product (e.g. "SLES for SAP Applications") a document
// applies to. It is distinct from the Distro: both can carry a version.
type Product struct {
	Name     string   `json:"name,omitempty" yaml:"name,omitempty"`
	Versions []string `json:"versions,omitempty" yaml:"versions,omitempty"`
}

// Chapter is a top level section of a document.
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

	// DocBook assemblies only; not exposed via MCP
	TopicType     string       `json:"topic_type,omitempty" yaml:"topic_type,omitempty"` // task, concept, reference, glue or container
	RenderAs      string       `json:"render_as,omitempty" yaml:"render_as,omitempty"`   // section, appendix, ...
	Maintainer    string       `json:"maintainer,omitempty" yaml:"maintainer,omitempty"`
	LegacySources []string     `json:"legacy_sources,omitempty" yaml:"legacy_sources,omitempty"` // "refers to" URLs of the original documentation
	Sections      []Section    `json:"sections,omitempty" yaml:"sections,omitempty"`
	Procedures    []Procedure  `json:"procedures,omitempty" yaml:"procedures,omitempty"`
	Admonitions   []Admonition `json:"admonitions,omitempty" yaml:"admonitions,omitempty"`
	Xrefs         []Xref       `json:"xrefs,omitempty" yaml:"xrefs,omitempty"`
	Links         []Link       `json:"links,omitempty" yaml:"links,omitempty"`
	Images        []Image      `json:"images,omitempty" yaml:"images,omitempty"`
	Includes      []string     `json:"includes,omitempty" yaml:"includes,omitempty"` // XIncluded snippet files

	// filled in by the parsers, not serialized
	LinesKey string `json:"-" yaml:"-"` // key of the source lines, see Corpus.AddLines
	Prose    string `json:"-" yaml:"-"` // text outside of code blocks, used for glossary matching and search
}

// GlossaryEntry is a term and its definition.
type GlossaryEntry struct {
	Term   string `json:"term" yaml:"term"`
	WhatIs string `json:"whatis" yaml:"whatis"`

	match   []*regexp.Regexp // one per alias, see glossaryMatchers
	needles []string         // one per alias, see glossaryNeedles
}

// DocSource records where a DocBook document came from.
type DocSource struct {
	DC        string `json:"dc" yaml:"dc"`                                   // DC file, relative to the corpus root
	Assembly  string `json:"assembly" yaml:"assembly"`                       // assembly file, relative to the corpus root
	Structure string `json:"structure,omitempty" yaml:"structure,omitempty"` // xml:id of the selected <structure>
}

// Profile is the profiling a DC file applies (PROFOS, PROFARCH, PROFCONDITION).
type Profile struct {
	OS        []string `json:"os,omitempty" yaml:"os,omitempty"`
	Arch      []string `json:"arch,omitempty" yaml:"arch,omitempty"`
	Condition []string `json:"condition,omitempty" yaml:"condition,omitempty"`
}

// DocMeta is the <meta> information of an assembly.
type DocMeta struct {
	Maintainer        string   `json:"maintainer,omitempty" yaml:"maintainer,omitempty"`
	Series            string   `json:"series,omitempty" yaml:"series,omitempty"`
	Tasks             []string `json:"tasks,omitempty" yaml:"tasks,omitempty"`
	Categories        []string `json:"categories,omitempty" yaml:"categories,omitempty"`
	Architectures     []string `json:"architectures,omitempty" yaml:"architectures,omitempty"`
	Updated           string   `json:"updated,omitempty" yaml:"updated,omitempty"`
	Title             string   `json:"title,omitempty" yaml:"title,omitempty"` // title for search engines
	Description       string   `json:"description,omitempty" yaml:"description,omitempty"`
	SocialDescription string   `json:"social_description,omitempty" yaml:"social_description,omitempty"`
}

// Revision is an entry of the revision history.
type Revision struct {
	Date        string `json:"date,omitempty" yaml:"date,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Bugtracker is where documentation bugs are reported (docmanager metadata).
type Bugtracker struct {
	URL       string `json:"url,omitempty" yaml:"url,omitempty"`
	Component string `json:"component,omitempty" yaml:"component,omitempty"`
	Product   string `json:"product,omitempty" yaml:"product,omitempty"`
	Assignee  string `json:"assignee,omitempty" yaml:"assignee,omitempty"`
}

// Section is a heading below the chapter heading. Level 1 is a direct
// subsection of the chapter. File is set for sections taken from another
// topic file (nested modules).
type Section struct {
	ID    string `json:"id,omitempty" yaml:"id,omitempty"`
	Title string `json:"title" yaml:"title"`
	Level int    `json:"level" yaml:"level"`
	File  string `json:"file,omitempty" yaml:"file,omitempty"`
}

// Procedure is a numbered list of steps.
type Procedure struct {
	ID    string `json:"id,omitempty" yaml:"id,omitempty"`
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
	Steps int    `json:"steps" yaml:"steps"`
}

// Admonition is a note, tip, important, warning or caution box.
type Admonition struct {
	Type  string `json:"type" yaml:"type"`
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
}

// Xref is a cross reference; Resolved is false if the target is not part of
// the document.
type Xref struct {
	Linkend  string `json:"linkend" yaml:"linkend"`
	Resolved bool   `json:"resolved" yaml:"resolved"`
}

// Link is an external link.
type Link struct {
	URL  string `json:"url" yaml:"url"`
	Text string `json:"text,omitempty" yaml:"text,omitempty"`
}

// Image is an image shown in the chapter; File is relative to the corpus root.
type Image struct {
	File string `json:"file" yaml:"file"`
	Alt  string `json:"alt,omitempty" yaml:"alt,omitempty"`
}

var reNonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a title into an id: "1.2 What's new?" -> "1-2-what-s-new".
func Slug(s string) string {
	s = strings.Trim(reNonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "chapter"
	}
	return s
}

// UniqueID returns id, or id with a numeric suffix if it was handed out before.
func UniqueID(seen map[string]int, id string) string {
	seen[id]++
	if n := seen[id]; n > 1 {
		return fmt.Sprintf("%s-%d", id, n)
	}
	return id
}

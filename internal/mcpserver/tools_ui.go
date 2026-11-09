package mcpserver

import (
	"html/template"

	"docullm/internal/docparse"
)

// The Build*PageData functions fill the PageData for each web page. They live
// next to the tool they render so the web presentation stays beside the MCP
// tool it mirrors. Each takes the query parameters of the page's form (mirroring
// the tool's input) and the corpus, and returns a PageData ready to execute the
// shell template.

// BuildIndexPageData fills the global index page (the tool overview). It is the
// only page that belongs to the global chrome rather than a single tool, but it
// is defined here so all page-data construction lives next to the tools.
func BuildIndexPageData(c *docparse.Corpus) PageData {
	ov := CorpusSummary(c)
	return PageData{
		Page:      "index",
		PageTitle: "Documentation tools",
		Summary:   &ov,
		Products:  ov.Products,
		Versions:  ov.Versions,
	}
}

// BuildListDocumentsPageData fills the documents page.
func BuildListDocumentsPageData(c *docparse.Corpus, product, version string) PageData {
	ov := CorpusSummary(c)
	docs := ListDocuments(c, product, version)
	return PageData{
		Page:      "documents",
		PageTitle: "Documents",
		Summary:   &ov,
		Products:  ov.Products,
		Versions:  ov.Versions,
		Product:   product,
		Version:   version,
		Docs:      &docs,
	}
}

// BuildReadDocumentPageData fills the document/chapter page. When chapterID is
// empty only the metadata and table of contents are shown; otherwise the
// chapter's markdown is rendered to HTML in ChapterHTML.
func BuildReadDocumentPageData(c *docparse.Corpus, docID, chapterID string) PageData {
	ov := CorpusSummary(c)
	out, err := BuildReadDocument(c, docID, chapterID)
	if err != nil {
		return PageData{Page: "read", PageTitle: "Read", Summary: &ov, Products: ov.Products, Versions: ov.Versions}
	}
	var chapterHTML template.HTML
	if out.Chapter != nil {
		chapterHTML = markdownToHTML(out.Chapter.Markdown)
	}
	return PageData{
		Page:        "read",
		PageTitle:   out.Title,
		Summary:     &ov,
		Products:    ov.Products,
		Versions:    ov.Versions,
		Read:        &out,
		ChapterHTML: chapterHTML,
	}
}

// BuildSearchPageData fills the search page. An empty query shows only the form.
func BuildSearchPageData(c *docparse.Corpus, query, product, version string, limit int) PageData {
	ov := CorpusSummary(c)
	var search *SearchDocsOut
	if query != "" {
		s := SearchDocs(c, query, product, version, limit)
		search = &s
	}
	return PageData{
		Page:      "search",
		PageTitle: "Search",
		Summary:   &ov,
		Products:  ov.Products,
		Versions:  ov.Versions,
		Query:     query,
		Product:   product,
		Version:   version,
		Limit:     limit,
		Search:    search,
	}
}

// BuildFindCommandPageData fills the find-command page.
func BuildFindCommandPageData(c *docparse.Corpus, pattern string) PageData {
	ov := CorpusSummary(c)
	var command *FindCommandOut
	if pattern != "" {
		f := FindCommand(c, pattern)
		command = &f
	}
	return PageData{
		Page:      "command",
		PageTitle: "Find command",
		Summary:   &ov,
		Products:  ov.Products,
		Versions:  ov.Versions,
		Pattern:   pattern,
		Command:   command,
	}
}

// BuildDefineTermPageData fills the define-term page.
func BuildDefineTermPageData(c *docparse.Corpus, term string) PageData {
	ov := CorpusSummary(c)
	var def *DefineTermOut
	if term != "" {
		d := DefineTerm(c, term)
		def = &d
	}
	return PageData{
		Page:      "term",
		PageTitle: "Define term",
		Summary:   &ov,
		Products:  ov.Products,
		Versions:  ov.Versions,
		TermQ:     term,
		Term:      def,
	}
}

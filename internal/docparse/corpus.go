package docparse

import (
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/hbollon/go-edlib"
)

// Corpus is the set of all parsed documents together with the merged glossary
// and the raw lines of every source file, so that chapter text can be served
// on demand.
type Corpus struct {
	Documents []Document      `json:"documents" yaml:"documents"`
	Glossary  []GlossaryEntry `json:"glossary" yaml:"glossary"`

	lines map[string][]string // source -> lines, see Chapter.LinesKey
	ids   map[string]int      // document ids handed out, see AddDocument
	df    map[string]int      // number of chapters containing a word, for search
	total int                 // number of chapters
}

// NewCorpus returns an empty corpus for a parser to fill with AddLines,
// AddDocument and AddGlossary. Finish must be called once it is complete.
func NewCorpus() *Corpus {
	return &Corpus{Documents: []Document{}, lines: map[string][]string{}, ids: map[string]int{}}
}

// AddLines stores the lines of a source; chapters refer to them by their
// LinesKey and their Start and End line.
func (c *Corpus) AddLines(key string, lines []string) {
	c.lines[key] = lines
}

// AddDocument appends doc, with a numeric suffix added to its id if the id is
// already taken.
func (c *Corpus) AddDocument(doc Document) {
	doc.ID = UniqueID(c.ids, doc.ID)
	c.Documents = append(c.Documents, doc)
}

// AddGlossary merges entries into the corpus glossary. The guides share their
// glossary, so the first definition of a term wins.
func (c *Corpus) AddGlossary(entries []GlossaryEntry) {
	for _, e := range entries {
		if c.term(e.Term) == nil {
			c.Glossary = append(c.Glossary, e)
		}
	}
}

func (c *Corpus) term(term string) *GlossaryEntry {
	for i := range c.Glossary {
		if strings.EqualFold(c.Glossary[i].Term, term) {
			return &c.Glossary[i]
		}
	}
	return nil
}

// Finish sorts the glossary, records which glossary terms every chapter
// mentions and builds the document frequencies used by search.
func (c *Corpus) Finish() {
	sort.Slice(c.Glossary, func(i, j int) bool {
		return strings.ToLower(c.Glossary[i].Term) < strings.ToLower(c.Glossary[j].Term)
	})
	for i := range c.Glossary {
		c.Glossary[i].match = glossaryMatchers(c.Glossary[i].Term)
		c.Glossary[i].needles = glossaryNeedles(c.Glossary[i].Term)
	}
	c.df = map[string]int{}
	// DocBook documents share topics, so the same prose shows up many times
	terms := map[string][]string{}
	for d := range c.Documents {
		for i := range c.Documents[d].Chapters {
			ch := &c.Documents[d].Chapters[i]
			t, ok := terms[ch.Prose]
			if !ok {
				t = c.termsIn(ch.Prose)
				terms[ch.Prose] = t
			}
			ch.Terms = t
			c.total++
			seen := map[string]bool{}
			for _, w := range words(ch.Title + " " + c.Text(ch)) {
				if !seen[w] {
					seen[w] = true
					c.df[w]++
				}
			}
		}
	}
}

var reAliasSplit = regexp.MustCompile(`\s*(?:,|\(|\))\s*`)

// aliases splits a glossary term into the names it can appear under:
// "CIB (cluster information base)" -> "CIB", "cluster information base" and
// "active/active, active/passive" -> "active/active", "active/passive".
func aliases(term string) []string {
	var out []string
	for _, a := range reAliasSplit.Split(term, -1) {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// glossaryMatchers builds one regexp per alias. Matches are on word
// boundaries, allow a plural "s"/"es" and treat a blank as blank or hyphen
// ("split brain" matches "split-brain"). Single words with capitals after the
// first letter (CIB, DC, QDevice, ReaR) are case sensitive, so that "DC" does
// not match "dc" and "ReaR" not "rear"; everything else is case insensitive.
func glossaryMatchers(term string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, a := range aliases(term) {
		parts := strings.Fields(a)
		for i, p := range parts {
			parts[i] = regexp.QuoteMeta(p)
		}
		flags := "(?i)"
		if len(parts) == 1 && strings.ToLower(a[1:]) != a[1:] {
			flags = ""
		}
		out = append(out, regexp.MustCompile(flags+`(?:^|[^\pL\pN_])`+strings.Join(parts, `[\s-]+`)+`(?:e?s)?(?:[^\pL\pN_]|$)`))
	}
	return out
}

// glossaryNeedles returns the lower case first word of every alias, which a
// text must contain for the corresponding matcher to match. Checking for it
// first saves running the (slow) matchers on most texts.
func glossaryNeedles(term string) []string {
	var out []string
	for _, a := range aliases(term) {
		out = append(out, strings.ToLower(strings.Fields(a)[0]))
	}
	return out
}

// termsIn returns the glossary terms mentioned in text, in glossary order.
func (c *Corpus) termsIn(text string) []string {
	var out []string
	lower := strings.ToLower(text)
	for _, e := range c.Glossary {
		for i, re := range e.match {
			if i < len(e.needles) && !strings.Contains(lower, e.needles[i]) {
				continue
			}
			if re.MatchString(text) {
				out = append(out, e.Term)
				break
			}
		}
	}
	return out
}

// Document returns the document with the given id, or nil.
func (c *Corpus) Document(id string) *Document {
	for i := range c.Documents {
		if c.Documents[i].ID == id {
			return &c.Documents[i]
		}
	}
	return nil
}

// Chapter returns the chapter with the given id, or nil.
func (d *Document) Chapter(id string) *Chapter {
	for i := range d.Chapters {
		if d.Chapters[i].ID == id {
			return &d.Chapters[i]
		}
	}
	return nil
}

// Text returns the markdown of the chapter.
func (c *Corpus) Text(ch *Chapter) string {
	lines := c.lines[ch.LinesKey]
	start, end := int(ch.Start)-1, min(int(ch.End), len(lines))
	if start < 0 || start >= end {
		return ""
	}
	return strings.Join(lines[start:end], "\n")
}

// Matches reports whether the document applies to product and version. An empty
// selector matches everything.
func (d *Document) Matches(product, version string) bool {
	if product != "" && !strings.Contains(strings.ToLower(d.Distro.Name), strings.ToLower(product)) {
		return false
	}
	return version == "" || slices.Contains(d.Distro.Versions, version)
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '-'
	})
}

// SearchHit is one ranked search result.
type SearchHit struct {
	DocID     string  `json:"doc_id"`
	ChapterID string  `json:"chapter_id"`
	Document  string  `json:"document"`
	Title     string  `json:"chapter_title"`
	Summary   string  `json:"summary,omitempty"`
	Snippet   string  `json:"snippet,omitempty"`
	Score     float64 `json:"score"`
}

// Search ranks chapters for query, optionally filtered by product and version,
// keeping at most limit hits (0 = no limit).
//
// By default the query is split into words and each word is scored against
// every chapter. A word matches corpus words within fuzzy edits of it (fuzzy
// 0 = exact only, 1 = a single typo, 2 = two). Chapters are ranked by the
// tf-idf of the matched words; an exact hit in the title or summary outweighs
// a hit in the body; and chapters matching all of the query words rank above
// those matching only some.
//
// If regex is set, the query is instead a regular expression (RE2 syntax)
// matched against each chapter's title, summary and text, and the matching
// chapters are returned in document order.
func (c *Corpus) Search(query, product, version string, limit, fuzzy int, regex bool) []SearchHit {
	if fuzzy < 0 {
		fuzzy = 0
	}
	if regex {
		return c.searchRegex(query, product, version, limit)
	}
	return c.searchWords(query, product, version, limit, fuzzy)
}

// searchWords scores each chapter that passes the product/version filter by
// how well the words of query match it, and returns them best first.
func (c *Corpus) searchWords(query, product, version string, limit, fuzzy int) []SearchHit {
	q := words(query)
	if len(q) == 0 {
		return nil
	}
	var hits []SearchHit
	for d := range c.Documents {
		doc := &c.Documents[d]
		if !doc.Matches(product, version) {
			continue
		}
		for i := range doc.Chapters {
			ch := &doc.Chapters[i]
			raw := c.Text(ch)
			score, matched := c.chapterScore(raw, words(ch.Title), words(ch.Summary), q, fuzzy)
			if matched > 0 {
				hits = append(hits, newSearchHit(doc, ch, raw, q, score))
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// chapterScore returns the tf-idf score of a chapter for the query words q and
// the number of those words it matched. The total is scaled by the fraction of
// query words matched, so a chapter containing all of them ranks above one
// containing only some.
func (c *Corpus) chapterScore(raw string, title, summary []string, q []string, fuzzy int) (score float64, matched int) {
	text := words(raw)
	for _, w := range q {
		s := c.wordScore(text, title, summary, w, fuzzy)
		if s > 0 {
			matched++
		}
		score += s
	}
	if matched > 0 {
		score *= float64(matched) / float64(len(q))
	}
	return score, matched
}

// wordScore is the tf-idf contribution of one query word w: idf(w) times the
// log of the number of (fuzzy) matches of w in the chapter text, with an exact
// match in the title counted three times and in the summary counted once.
func (c *Corpus) wordScore(text, title, summary []string, w string, fuzzy int) float64 {
	idf := math.Log(1 + float64(c.total) / float64(1+c.df[w]))
	tf := math.Log(1 + float64(fuzzyCount(text, w, fuzzy)))
	return (tf + 3*float64(count(title, w)) + float64(count(summary, w))) * idf
}

// newSearchHit assembles the hit for chapter ch of document doc.
func newSearchHit(doc *Document, ch *Chapter, raw string, q []string, score float64) SearchHit {
	return SearchHit{
		DocID: doc.ID, ChapterID: ch.ID, Document: doc.Title, Title: ch.Title,
		Summary: ch.Summary, Snippet: snippet(raw, q), Score: math.Round(score*100) / 100,
	}
}

// searchRegex matches the regular expression query against each chapter's
// title, summary and text and returns the hits in document order. An invalid
// pattern yields no hits.
func (c *Corpus) searchRegex(query, product, version string, limit int) []SearchHit {
	re, err := regexp.Compile(query)
	if err != nil {
		return nil
	}
	var hits []SearchHit
	for d := range c.Documents {
		doc := &c.Documents[d]
		if !doc.Matches(product, version) {
			continue
		}
		for i := range doc.Chapters {
			ch := &doc.Chapters[i]
			raw := c.Text(ch)
			if re.MatchString(ch.Title) || re.MatchString(ch.Summary) || re.MatchString(raw) {
				hits = append(hits, newSearchHit(doc, ch, raw, words(query), 0))
			}
			if limit > 0 && len(hits) >= limit {
				return hits
			}
		}
	}
	return hits
}

// fuzzyCount counts the words within fuzzy edits of w. A distance of 0 counts
// only exact matches (the same as the word frequency), 1 also a single
// character typo, 2 also two.
func fuzzyCount(text []string, w string, fuzzy int) int {
	n := 0
	for _, v := range text {
		if edlib.LevenshteinDistance(w, v) <= fuzzy {
			n++
		}
	}
	return n
}

func count(list []string, s string) int {
	n := 0
	for _, v := range list {
		if v == s {
			n++
		}
	}
	return n
}

// snippet returns the line of text containing the most query words, skipping
// the heading.
func snippet(text string, q []string) string {
	best, bestN := "", 0
	for i, ln := range strings.Split(text, "\n") {
		if i == 0 {
			continue
		}
		ws := words(ln)
		n := 0
		for _, w := range q {
			if count(ws, w) > 0 {
				n++
			}
		}
		if n > bestN {
			best, bestN = strings.TrimSpace(ln), n
		}
	}
	if r := []rune(best); len(r) > 240 {
		best = string(r[:240]) + "…"
	}
	return best
}

// CommandHit locates a shell command within a chapter.
type CommandHit struct {
	Command   string `json:"command"`
	DocID     string `json:"doc_id"`
	ChapterID string `json:"chapter_id"`
	Title     string `json:"chapter_title"`
}

// FindCommand returns the commands that contain every word of pattern, case
// insensitive.
func (c *Corpus) FindCommand(pattern string) []CommandHit {
	q := strings.Fields(strings.ToLower(pattern))
	var out []CommandHit
	for _, doc := range c.Documents {
		for _, ch := range doc.Chapters {
		cmds:
			for _, cmd := range ch.Commands {
				l := strings.ToLower(cmd)
				for _, w := range q {
					if !strings.Contains(l, w) {
						continue cmds
					}
				}
				out = append(out, CommandHit{Command: cmd, DocID: doc.ID, ChapterID: ch.ID, Title: ch.Title})
			}
		}
	}
	return out
}

// DefineTerm looks up a term by its full name or one of its aliases. If
// nothing matches exactly, all terms containing the query are returned.
func (c *Corpus) DefineTerm(term string) []GlossaryEntry {
	t := strings.ToLower(strings.TrimSpace(term))
	for _, e := range c.Glossary {
		if strings.ToLower(e.Term) == t {
			return []GlossaryEntry{e}
		}
		for _, a := range aliases(e.Term) {
			if strings.ToLower(a) == t {
				return []GlossaryEntry{e}
			}
		}
	}
	var out []GlossaryEntry
	for _, e := range c.Glossary {
		if t != "" && strings.Contains(strings.ToLower(e.Term), t) {
			out = append(out, e)
		}
	}
	return out
}

// GlossaryFor returns the glossary entries for the given terms.
func (c *Corpus) GlossaryFor(terms []string) []GlossaryEntry {
	out := []GlossaryEntry{}
	for _, t := range terms {
		if e := c.term(t); e != nil {
			out = append(out, *e)
		}
	}
	return out
}

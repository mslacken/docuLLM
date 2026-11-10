package docparse

import (
	"regexp"
	"slices"
	"strings"
)

var (
	reLicenseTitle = regexp.MustCompile(`(?i)^GNU Free Documentation License`)
	reLegal        = regexp.MustCompile(`(?i)legal notice`)
)

// IsLegalNotice reports whether a section title is the legal notice, which
// holds the license terms of the document and is not a chapter.
func IsLegalNotice(title string) bool { return reLegal.MatchString(title) }

// IsLicenseTitle reports whether a title starts the full license text.
func IsLicenseTitle(title string) bool { return reLicenseTitle.MatchString(title) }

// License returns the SPDX expression for a document from the text of its
// legal notice or, if that has none, from the full license text it ships.
func License(notice, licenseText string) string {
	if l := spdx(notice); l != "" {
		return l
	}
	return spdxFromLicenseText(licenseText)
}

var (
	reGFDL        = regexp.MustCompile(`(?i)GNU Free Documentation License`)
	reGFDLVersion = regexp.MustCompile(`(?i)\bversion\s+(1\.[123])\b`)
	reLaterVer    = regexp.MustCompile(`(?i)any later version`)
	reInvariants  = regexp.MustCompile(`(?i)with (?:the )?Invariant Sections? being`)
	reNoInvariant = regexp.MustCompile(`(?i)with no Invariant Sections`)

	otherLicenses = []struct {
		re   *regexp.Regexp
		spdx string
	}{
		{regexp.MustCompile(`(?i)Creative Commons Attribution[- ]Share[- ]?Alike 4\.0`), "CC-BY-SA-4.0"},
		{regexp.MustCompile(`(?i)Creative Commons Attribution 4\.0`), "CC-BY-4.0"},
		{regexp.MustCompile(`(?i)Apache License,? Version 2\.0`), "Apache-2.0"},
		{regexp.MustCompile(`(?i)GNU General Public License,? version 2`), "GPL-2.0-only"},
		{regexp.MustCompile(`(?i)GNU General Public License,? version 3`), "GPL-3.0-only"},
		{regexp.MustCompile(`(?i)\bMIT License\b`), "MIT"},
	}
)

// spdx maps a license notice like "GNU Free Documentation License, Version 1.2
// or (at your option) version 1.3; with the Invariant Section being ..." to an
// SPDX expression.
func spdx(notice string) string {
	if notice == "" {
		return ""
	}
	if reGFDL.MatchString(notice) {
		var versions []string
		for _, m := range reGFDLVersion.FindAllStringSubmatch(notice, -1) {
			if !slices.Contains(versions, m[1]) {
				versions = append(versions, m[1])
			}
		}
		if len(versions) == 0 {
			versions = []string{"1.3"}
		}
		inv := ""
		switch {
		case reInvariants.MatchString(notice):
			inv = "-invariants"
		case reNoInvariant.MatchString(notice):
			inv = "-no-invariants"
		}
		suffix := "-only"
		if reLaterVer.MatchString(notice) {
			suffix = "-or-later"
		}
		ids := make([]string, len(versions))
		for i, v := range versions {
			ids[i] = "GFDL-" + v + inv + suffix
		}
		return strings.Join(ids, " OR ")
	}
	for _, l := range otherLicenses {
		if l.re.MatchString(notice) {
			return l.spdx
		}
	}
	return ""
}

// spdxFromLicenseText is the fallback when there is no legal notice: the full
// license text only tells us which license and version is shipped, its
// addendum template must not be mistaken for the document's own terms.
func spdxFromLicenseText(text string) string {
	if reGFDL.MatchString(text) {
		if m := reGFDLVersion.FindStringSubmatch(text); m != nil {
			return "GFDL-" + m[1] + "-only"
		}
		return ""
	}
	return spdx(text)
}

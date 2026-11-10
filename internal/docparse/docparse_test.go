package docparse

import (
	"reflect"
	"testing"
)

func TestAliases(t *testing.T) {
	for term, want := range map[string][]string{
		"CIB (cluster information base)": {"CIB", "cluster information base"},
		"active/active, active/passive":  {"active/active", "active/passive"},
		"Pacemaker":                      {"Pacemaker"},
	} {
		if got := aliases(term); !reflect.DeepEqual(got, want) {
			t.Errorf("aliases(%q) = %q, want %q", term, got, want)
		}
	}
}

func TestGlossaryMatchers(t *testing.T) {
	for _, tc := range []struct {
		term, text string
		want       bool
	}{
		{"CIB (cluster information base)", "the CIB is replicated", true},
		{"CIB (cluster information base)", "the Cluster Information Base", true},
		{"CIB (cluster information base)", "cib", false},
		{"DC (designated coordinator)", "cd /etc/dc", false},
		{"ReaR (Relax and Recover)", "the rear node", false},
		{"split brain", "avoid a split-brain scenario", true},
		{"clone", "two clones of it", true},
		{"node", "nodes", true},
		{"node", "nodejs", false},
		{"location constraint", "add a location   constraint", true},
	} {
		got := false
		for _, re := range glossaryMatchers(tc.term) {
			got = got || re.MatchString(tc.text)
		}
		if got != tc.want {
			t.Errorf("%q in %q = %v, want %v", tc.term, tc.text, got, tc.want)
		}
	}
}

func TestUniqueID(t *testing.T) {
	seen := map[string]int{}
	got := []string{UniqueID(seen, Slug("Overview")), UniqueID(seen, Slug("Overview")), UniqueID(seen, Slug("1.2 What's new?"))}
	want := []string{"overview", "overview-2", "1-2-what-s-new"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

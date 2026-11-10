package markdown

import (
	"strings"
	"testing"
)

func TestProseSkipsCode(t *testing.T) {
	prose := proseOf(parseNodes(strings.Split("## Title\n\nUse `crm` here.\n\n```\n# crm configure\n```\n", "\n")))
	if strings.Contains(prose, "crm") || !strings.Contains(prose, "Use  here.") {
		t.Errorf("prose = %q", prose)
	}
}

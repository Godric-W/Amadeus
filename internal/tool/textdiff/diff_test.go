package textdiff

import (
	"strings"
	"testing"
)

func TestContentDiffPreservesEmptyAndFinalNewlineSemantics(t *testing.T) {
	diff := ContentDiff("edit", "/tmp/file.txt", "", nil, []byte("value"))
	for _, fragment := range []string{
		"--- /tmp/file.txt\n",
		"+++ /tmp/file.txt\n",
		"@@ -0,0 +1,1 @@\n",
		"+value\n",
		"\\ No newline at end of file\n",
	} {
		if !strings.Contains(diff, fragment) {
			t.Fatalf("diff %q does not contain %q", diff, fragment)
		}
	}
}

func TestChangedLineStatsCountsOnlyChangedRegion(t *testing.T) {
	stats := ChangedLineStats([]byte("same\nold\nend\n"), []byte("same\nnew\nextra\nend\n"))
	if stats.Insertions != 2 || stats.Deletions != 1 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func TestContentDiffMarksBinaryContent(t *testing.T) {
	diff := ContentDiff("edit", "/tmp/file.bin", "", []byte{0x00}, []byte{0x01})
	if !strings.Contains(diff, "Binary files differ") {
		t.Fatalf("binary diff omitted marker: %q", diff)
	}
}

package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"testing"

	"github.com/Godric-W/Amadeus/internal/prompt/builtin"
	toolbuiltin "github.com/Godric-W/Amadeus/internal/tool/builtin"
)

func TestSourceManifestIsCompleteAndDefensive(t *testing.T) {
	if err := ValidateSourceManifest(); err != nil {
		t.Fatal(err)
	}
	first := SourceManifest()
	if len(first) == 0 {
		t.Fatal("source manifest is empty")
	}
	first[0].ID = "changed"
	first[len(first)-1].Tools[0] = "changed"
	if SourceManifest()[0].ID == "changed" {
		t.Fatal("source manifest shares mutable storage")
	}
	if SourceManifest()[len(first)-1].Tools[0] == "changed" {
		t.Fatal("source manifest shares mutable Tool mappings")
	}
}

func TestSourceManifestMapsEveryBuiltinToolExactlyOnce(t *testing.T) {
	mapped := make([]string, 0)
	for _, asset := range SourceManifest() {
		mapped = append(mapped, asset.Tools...)
	}
	want := make([]string, 0)
	for _, entry := range toolbuiltin.TargetCatalog() {
		if entry.Status == toolbuiltin.CatalogAvailable {
			want = append(want, entry.Name)
		}
	}
	sort.Strings(mapped)
	sort.Strings(want)
	if len(mapped) != len(want) {
		t.Fatalf("source Tool mapping count = %d, want %d: mapped=%v want=%v", len(mapped), len(want), mapped, want)
	}
	for index := range want {
		if mapped[index] != want[index] {
			t.Fatalf("source Tool mapping = %v, want %v", mapped, want)
		}
	}
}

func TestAdaptedPromptAssetsMatchManifest(t *testing.T) {
	ids := map[string]builtin.ID{
		"model.default":             builtin.AgentBase,
		"mode.default":              builtin.ModeExecute,
		"mode.plan":                 builtin.ModePlan,
		"compact.prompt":            builtin.ContextCompaction,
		"compact.summary_prefix":    builtin.ContextCompactionPrefix,
		"multi_agent.role.subagent": builtin.AgentSubagent,
	}
	for _, asset := range SourceManifest() {
		id, exists := ids[asset.ID]
		if !exists {
			continue
		}
		content, err := builtin.Read(id)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(content))
		if got := hex.EncodeToString(digest[:]); got != asset.AdaptedSHA256 {
			t.Fatalf("adapted asset %q hash = %s, want %s", asset.ID, got, asset.AdaptedSHA256)
		}
	}
}

func TestSourceManifestCoversEveryToolFamily(t *testing.T) {
	seen := map[SourceFamily]bool{}
	for _, asset := range SourceManifest() {
		seen[asset.Family] = true
	}
	for _, family := range []SourceFamily{SourceCodex, SourceClaudeCode, SourceAmadeus} {
		if !seen[family] {
			t.Fatalf("source manifest does not cover %q", family)
		}
	}
}

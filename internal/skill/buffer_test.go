package skill

import (
	"strings"
	"testing"
)

func TestContextBufferDeduplicatesBoundsAndConsumesOnce(t *testing.T) {
	buffer, err := NewContextBuffer(BufferOptions{MaxSkills: 1, MaxBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	catalog := &Catalog{values: map[string]Skill{
		"one": {Name: "one", Description: "first", Content: "body", Source: SourceProject},
		"two": {Name: "two", Description: "second", Content: "body", Source: SourceProject},
	}}
	if _, err := buffer.Load(catalog, "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := buffer.Load(catalog, "one"); err != nil || buffer.Len() != 1 {
		t.Fatalf("duplicate load = %v, len=%d", err, buffer.Len())
	}
	if _, err := buffer.Load(catalog, "two"); err == nil {
		t.Fatal("expected skill count budget")
	}
	values := buffer.Consume()
	if len(values) != 1 || values[0].Name != "one" || buffer.Len() != 0 || len(buffer.Consume()) != 0 {
		t.Fatalf("unexpected consume result: %#v", values)
	}
}

func TestMarshalContextIncludesBodiesInStableOrder(t *testing.T) {
	content, err := MarshalContext([]Skill{
		{Name: "zeta", Description: "Zeta", Content: "Z body", Source: SourceUser},
		{Name: "alpha", Description: "Alpha", Content: "A body", Source: SourceProject},
	})
	if err != nil {
		t.Fatalf("marshal Skill context: %v", err)
	}
	if !strings.Contains(content, `"type":"amadeus.skill_context.v1"`) || !strings.Contains(content, "A body") || !strings.Contains(content, "Z body") {
		t.Fatalf("Skill context missing payload: %s", content)
	}
	if strings.Index(content, `"name":"alpha"`) > strings.Index(content, `"name":"zeta"`) {
		t.Fatalf("Skill context order is unstable: %s", content)
	}
}

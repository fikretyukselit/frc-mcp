package chunking

import (
	"strings"
	"testing"
)

func TestSplitNeverBreaksFence(t *testing.T) {
	code := "```java\n" + strings.Repeat("x();\n\n", 800) + "```"
	for _, part := range Split(strings.Repeat("para.\n\n", 50) + code + "\n\ntrailing") {
		if strings.Count(part, "```")%2 != 0 {
			t.Fatalf("fence split: ...%s", part[max(0, len(part)-40):])
		}
	}
}

func TestVariants(t *testing.T) {
	vs := Variants([]Block{{Text: "shared"}, {Lang: "java", Text: "J"}, {Lang: "cpp", Text: "C"}})
	if len(vs) != 2 || vs[0].Lang != "cpp" || vs[0].Text != "shared\n\nC" || vs[1].Text != "shared\n\nJ" {
		t.Fatalf("%+v", vs)
	}
	if vs := Variants([]Block{{Text: "a"}}); len(vs) != 1 || vs[0].Lang != "any" {
		t.Fatalf("%+v", vs)
	}
}

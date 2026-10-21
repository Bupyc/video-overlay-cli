package layout

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseWritePreservesUnknownXML(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<layout custom="&amp;" xmlns:x="urn:unknown">
  <!-- keep this comment -->
  <composite name="panel"><component type="gradient-chart" mystery="yes">hello &amp; world<x:unknown k="v"/></component></composite>
</layout>
`
	doc, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Root.Tag != "layout" || doc.Root.Location.Line != 2 || doc.Root.Location.Column != 1 {
		t.Fatalf("unexpected root: %#v", doc.Root)
	}
	component := doc.Root.Children[0].Children[0]
	if component.Tag != "component" || component.Text != "hello & world" {
		t.Fatalf("unexpected component: %#v", component)
	}
	if got := attributeValue(component, "type"); got != "gradient-chart" {
		t.Fatalf("component type = %q", got)
	}
	component.Attributes[1].Value = "updated"
	component.Text = "edited label"
	component.Children = append(component.Children, &Element{Tag: "editor-added"})

	var output bytes.Buffer
	if err := Write(&output, doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `xmlns:x="urn:unknown"`) ||
		!strings.Contains(output.String(), `<!-- keep this comment -->`) ||
		!strings.Contains(output.String(), `<x:unknown k="v"/>`) ||
		!strings.Contains(output.String(), `mystery="updated"`) ||
		!strings.Contains(output.String(), `<editor-added/>`) {
		t.Fatalf("round-trip dropped XML content:\n%s", output.String())
	}

	parsed, err := Parse(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Root.Children[0].Children[0].Text; got != "edited label" {
		t.Fatalf("round-trip text = %q", got)
	}
	if got := len(parsed.Root.Children[0].Children[0].Children); got != 2 {
		t.Fatalf("round-trip child count = %d, want 2", got)
	}
}

func TestNormalizeType(t *testing.T) {
	for input, want := range map[string]string{
		"gradient-chart":    "gradient_chart",
		"cairo-gauge-donut": "cairo_gauge_donut",
		"chart":             "chart",
	} {
		if got := NormalizeType(input); got != want {
			t.Errorf("NormalizeType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateReportsUnsupportedComponentLocation(t *testing.T) {
	doc, err := Parse(strings.NewReader("<layout>\n  <component type=\"not-supported\"/>\n</layout>"))
	if err != nil {
		t.Fatal(err)
	}
	err = Validate(doc, func(componentType string) bool { return componentType == "chart" })
	if err == nil || !strings.Contains(err.Error(), `not-supported`) || !strings.Contains(err.Error(), "line 2, column 3") {
		t.Fatalf("Validate error = %v", err)
	}
}

func TestValidateNormalizesHyphenatedComponentType(t *testing.T) {
	doc, err := Parse(strings.NewReader(`<layout><component type="gradient-chart"/></layout>`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(doc, func(componentType string) bool { return componentType == "gradient_chart" }); err != nil {
		t.Fatal(err)
	}
}

func TestParseRejectsMismatchedElements(t *testing.T) {
	if _, err := Parse(strings.NewReader("<layout><component></layout>")); err == nil {
		t.Fatal("Parse accepted mismatched elements")
	}
}

func TestParseRejectsTextOutsideRoot(t *testing.T) {
	if _, err := Parse(strings.NewReader("junk<layout/>")); err == nil {
		t.Fatal("Parse accepted character data outside the root element")
	}
}

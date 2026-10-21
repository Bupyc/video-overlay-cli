package layout

import "strings"

type Document struct {
	Root   *Element
	Prolog []Node
	Epilog []Node
}

type Element struct {
	Tag        string
	Attributes []Attribute
	Text       string
	Children   []*Element
	Location   Location
	Content    []Node
}

type Attribute struct {
	Name  string
	Value string
}

type Location struct {
	Line   int
	Column int
}

type NodeKind uint8

const (
	TextNode NodeKind = iota
	ElementNode
	CommentNode
	ProcInstNode
	DirectiveNode
)

type Node struct {
	Kind    NodeKind
	Text    string
	Target  string
	Element *Element
}

func NormalizeType(value string) string {
	return strings.ReplaceAll(value, "-", "_")
}

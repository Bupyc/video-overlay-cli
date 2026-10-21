package layout

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

func Parse(r io.Reader) (*Document, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read layout XML: %w", err)
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	doc := &Document{}
	var stack []*Element

	for {
		token, err := decoder.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse layout XML: %w", err)
		}
		switch token := token.(type) {
		case xml.StartElement:
			element := &Element{Tag: name(token.Name), Location: locationAt(data, decoder.InputOffset())}
			for _, attr := range token.Attr {
				element.Attributes = append(element.Attributes, Attribute{Name: name(attr.Name), Value: attr.Value})
			}
			if len(stack) == 0 {
				if doc.Root != nil {
					return nil, fmt.Errorf("multiple root elements at line %d, column %d", element.Location.Line, element.Location.Column)
				}
				doc.Root = element
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, element)
				parent.Content = append(parent.Content, Node{Kind: ElementNode, Element: element})
			}
			stack = append(stack, element)
		case xml.EndElement:
			loc := locationAt(data, decoder.InputOffset())
			closing := name(token.Name)
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected closing element </%s> at line %d, column %d", closing, loc.Line, loc.Column)
			}
			open := stack[len(stack)-1]
			if open.Tag != closing {
				return nil, fmt.Errorf("mismatched closing element </%s> at line %d, column %d; expected </%s>", closing, loc.Line, loc.Column, open.Tag)
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 && len(bytes.TrimSpace(token)) != 0 {
				return nil, fmt.Errorf("character data outside the root element")
			}
			node := Node{Kind: TextNode, Text: string(token)}
			appendNode(doc, stack, node)
		case xml.Comment:
			appendNode(doc, stack, Node{Kind: CommentNode, Text: string(token)})
		case xml.ProcInst:
			appendNode(doc, stack, Node{Kind: ProcInstNode, Target: token.Target, Text: string(token.Inst)})
		case xml.Directive:
			appendNode(doc, stack, Node{Kind: DirectiveNode, Text: string(token)})
		}
	}
	if len(stack) != 0 {
		open := stack[len(stack)-1]
		return nil, fmt.Errorf("unclosed element <%s> at line %d, column %d", open.Tag, open.Location.Line, open.Location.Column)
	}
	if doc.Root == nil {
		return nil, fmt.Errorf("layout XML has no root element")
	}
	return doc, nil
}

func Write(w io.Writer, doc *Document) error {
	if doc == nil || doc.Root == nil {
		return fmt.Errorf("write layout XML: document has no root element")
	}
	writer := bufio.NewWriter(w)
	for _, node := range doc.Prolog {
		if err := writeNode(writer, node); err != nil {
			return fmt.Errorf("write layout XML prolog: %w", err)
		}
	}
	if err := writeElement(writer, doc.Root); err != nil {
		return fmt.Errorf("write layout XML: %w", err)
	}
	for _, node := range doc.Epilog {
		if err := writeNode(writer, node); err != nil {
			return fmt.Errorf("write layout XML epilog: %w", err)
		}
	}
	return writer.Flush()
}

func appendNode(doc *Document, stack []*Element, node Node) {
	if len(stack) == 0 {
		if doc.Root == nil {
			doc.Prolog = append(doc.Prolog, node)
		} else {
			doc.Epilog = append(doc.Epilog, node)
		}
		return
	}
	element := stack[len(stack)-1]
	element.Content = append(element.Content, node)
	if node.Kind == TextNode {
		element.Text += node.Text
	}
}

func name(value xml.Name) string {
	if value.Space != "" {
		return value.Space + ":" + value.Local
	}
	return value.Local
}

func locationAt(data []byte, offset int64) Location {
	if offset < 0 || offset > int64(len(data)) {
		offset = int64(len(data))
	}
	end := int(offset)
	start := bytes.LastIndexByte(data[:end], '<')
	if start < 0 {
		start = 0
	}
	line := 1 + bytes.Count(data[:start], []byte{'\n'})
	lastNewline := bytes.LastIndexByte(data[:start], '\n')
	return Location{Line: line, Column: utf8.RuneCount(data[lastNewline+1:start]) + 1}
}

func writeNode(w io.Writer, node Node) error {
	switch node.Kind {
	case TextNode:
		return xml.EscapeText(w, []byte(node.Text))
	case ElementNode:
		if node.Element == nil {
			return fmt.Errorf("element node has no element")
		}
		return writeElement(w, node.Element)
	case CommentNode:
		if strings.Contains(node.Text, "--") || strings.HasSuffix(node.Text, "-") {
			return fmt.Errorf("invalid XML comment")
		}
		_, err := io.WriteString(w, "<!--"+node.Text+"-->")
		return err
	case ProcInstNode:
		if node.Target == "" || strings.Contains(node.Text, "?>") {
			return fmt.Errorf("invalid processing instruction")
		}
		instruction := "<?" + node.Target
		if node.Text != "" {
			instruction += " " + node.Text
		}
		_, err := io.WriteString(w, instruction+"?>")
		return err
	case DirectiveNode:
		_, err := io.WriteString(w, "<!"+node.Text+">")
		return err
	default:
		return fmt.Errorf("unknown XML node kind %d", node.Kind)
	}
}

var attributeEscaper = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", `"`, "&#34;", "\r", "&#13;", "\n", "&#10;", "\t", "&#9;",
)

func writeElement(w io.Writer, element *Element) error {
	if element == nil || element.Tag == "" {
		return fmt.Errorf("element has no tag")
	}
	if _, err := io.WriteString(w, "<"+element.Tag); err != nil {
		return err
	}
	for _, attr := range element.Attributes {
		if attr.Name == "" {
			return fmt.Errorf("element <%s> has an attribute with no name", element.Tag)
		}
		if _, err := io.WriteString(w, " "+attr.Name+`="`+attributeEscaper.Replace(attr.Value)+`"`); err != nil {
			return err
		}
	}
	if len(element.Content) == 0 && len(element.Children) == 0 && element.Text == "" {
		_, err := io.WriteString(w, "/>")
		return err
	}
	if _, err := io.WriteString(w, ">"); err != nil {
		return err
	}
	if len(element.Content) != 0 {
		textChanged := contentText(element.Content) != element.Text
		childrenChanged := !contentChildrenMatch(element.Content, element.Children)
		textWritten := false
		childIndex := 0
		for _, node := range element.Content {
			if textChanged && node.Kind == TextNode {
				if !textWritten && element.Text != "" {
					if err := xml.EscapeText(w, []byte(element.Text)); err != nil {
						return err
					}
				}
				textWritten = true
				continue
			}
			if childrenChanged && node.Kind == ElementNode {
				if childIndex < len(element.Children) {
					if err := writeElement(w, element.Children[childIndex]); err != nil {
						return err
					}
				}
				childIndex++
				continue
			}
			if err := writeNode(w, node); err != nil {
				return err
			}
		}
		if textChanged && !textWritten && element.Text != "" {
			if err := xml.EscapeText(w, []byte(element.Text)); err != nil {
				return err
			}
		}
		for ; childIndex < len(element.Children); childIndex++ {
			if err := writeElement(w, element.Children[childIndex]); err != nil {
				return err
			}
		}
	} else {
		if err := xml.EscapeText(w, []byte(element.Text)); err != nil {
			return err
		}
		for _, child := range element.Children {
			if err := writeElement(w, child); err != nil {
				return err
			}
		}
	}
	_, err := io.WriteString(w, "</"+element.Tag+">")
	return err
}

func contentText(nodes []Node) string {
	var text strings.Builder
	for _, node := range nodes {
		if node.Kind == TextNode {
			text.WriteString(node.Text)
		}
	}
	return text.String()
}

func contentChildrenMatch(nodes []Node, children []*Element) bool {
	index := 0
	for _, node := range nodes {
		if node.Kind != ElementNode {
			continue
		}
		if index >= len(children) || node.Element != children[index] {
			return false
		}
		index++
	}
	return index == len(children)
}

package layout

import "fmt"

func Validate(doc *Document, supported func(componentType string) bool) error {
	if doc == nil || doc.Root == nil {
		return fmt.Errorf("validate layout: document has no root element")
	}
	if supported == nil {
		return fmt.Errorf("validate layout: supported-component predicate is nil")
	}
	stack := []*Element{doc.Root}
	for len(stack) != 0 {
		element := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if element.Tag == "component" {
			componentType := attributeValue(element, "type")
			if componentType == "" {
				return fmt.Errorf("component has no type at line %d, column %d", element.Location.Line, element.Location.Column)
			}
			if !supported(NormalizeType(componentType)) {
				return fmt.Errorf("component type %q is not supported at line %d, column %d", componentType, element.Location.Line, element.Location.Column)
			}
		}
		for i := len(element.Children) - 1; i >= 0; i-- {
			stack = append(stack, element.Children[i])
		}
	}
	return nil
}

func attributeValue(element *Element, name string) string {
	for _, attr := range element.Attributes {
		if attr.Name == name {
			return attr.Value
		}
	}
	return ""
}

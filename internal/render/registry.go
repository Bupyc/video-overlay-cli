package render

import (
	"fmt"

	"video-overlay-cli/internal/layout"
)

type Registry struct {
	factories map[string]Factory
	deferred  map[string]struct{}
}

func NewRegistry() Registry {
	return Registry{factories: make(map[string]Factory), deferred: make(map[string]struct{})}
}

func (r *Registry) Register(componentType string, factory Factory) error {
	componentType = layout.NormalizeType(componentType)
	if componentType == "" || factory == nil {
		return fmt.Errorf("component type and factory are required")
	}
	if r.factories == nil {
		r.factories = make(map[string]Factory)
	}
	if _, exists := r.factories[componentType]; exists {
		return fmt.Errorf("component type %q is already registered", componentType)
	}
	r.factories[componentType] = factory
	delete(r.deferred, componentType)
	return nil
}

func (r *Registry) MarkDeferred(componentTypes ...string) {
	if r.deferred == nil {
		r.deferred = make(map[string]struct{})
	}
	for _, componentType := range componentTypes {
		componentType = layout.NormalizeType(componentType)
		if componentType != "" {
			if _, registered := r.factories[componentType]; !registered {
				r.deferred[componentType] = struct{}{}
			}
		}
	}
}

func (r Registry) IsSupported(componentType string) bool {
	_, ok := r.factories[layout.NormalizeType(componentType)]
	return ok
}

func (r Registry) Status(componentType string) string {
	componentType = layout.NormalizeType(componentType)
	if _, ok := r.factories[componentType]; ok {
		return "supported"
	}
	if _, ok := r.deferred[componentType]; ok {
		return "deferred"
	}
	return "unsupported"
}

func (r Registry) factory(componentType string) (Factory, bool) {
	factory, ok := r.factories[layout.NormalizeType(componentType)]
	return factory, ok
}

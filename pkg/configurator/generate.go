package configurator

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var durationType = reflect.TypeOf(time.Duration(0))

// GenerateYAML renders o's type into an annotated YAML config template: keys come
// from `mapstructure` tags, values from `default` tags (else the type's zero
// value), and `description` tags become line comments. Nested structs recurse; a
// map emits one placeholder entry keyed by its (cleaned) `mapkey` token. Fields
// tagged hidden:"true", unexported, or without a `mapstructure` tag are skipped.
// The output round-trips: decoding it back into o yields the documented defaults.
func GenerateYAML(o interface{}) ([]byte, error) {
	t := reflect.TypeOf(o)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("configurator: GenerateYAML requires a struct, got %s", t.Kind())
	}
	node, err := structNode(t)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(node)
}

// structNode builds a YAML mapping for every configurable field of t.
func structNode(t reflect.Type) (*yaml.Node, error) {
	m := &yaml.Node{Kind: yaml.MappingNode}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name := field.Tag.Get("mapstructure")
		if !field.IsExported() || name == "" || name == "-" {
			continue
		}
		if strings.EqualFold(field.Tag.Get("hidden"), "true") {
			continue
		}
		val, err := valueNode(field.Type, field.Tag)
		if err != nil {
			return nil, err
		}
		key := &yaml.Node{Kind: yaml.ScalarNode, Value: name}
		// Attach the description to the value node so it renders as a trailing
		// comment for scalars and flow sequences alike (a key-node comment is
		// dropped on an inline `[]` value by yaml.v3).
		if desc := field.Tag.Get("description"); desc != "" {
			val.LineComment = desc
		}
		m.Content = append(m.Content, key, val)
	}
	return m, nil
}

// valueNode builds the value node for a field of type t with the given tags.
func valueNode(t reflect.Type, tag reflect.StructTag) (*yaml.Node, error) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	def := tag.Get("default")

	// time.Duration is an int64 kind but must render as a string like "30m".
	if t == durationType {
		return strNode(orElse(def, "0s")), nil
	}

	switch t.Kind() {
	case reflect.Struct:
		return structNode(t)
	case reflect.Map:
		return mapNode(t, tag)
	case reflect.Slice, reflect.Array:
		return &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}, nil
	case reflect.String:
		return strNode(def), nil
	case reflect.Bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: orElse(def, "false")}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: orElse(def, "0")}, nil
	default:
		return nil, fmt.Errorf("configurator: unsupported field type %s", t.Kind())
	}
}

// mapNode emits a single placeholder entry so the shape of a map[string]T section
// is visible in the template.
func mapNode(t reflect.Type, tag reflect.StructTag) (*yaml.Node, error) {
	key := strings.Trim(orElse(tag.Get("mapkey"), DEFAULT_MAPKEY), "<>")
	elem := t.Elem()
	for elem.Kind() == reflect.Ptr {
		elem = elem.Elem()
	}
	var entry *yaml.Node
	var err error
	if elem.Kind() == reflect.Struct {
		entry, err = structNode(elem)
	} else {
		entry, err = valueNode(elem, "")
	}
	if err != nil {
		return nil, err
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key, LineComment: "example entry — rename to your key"}
	return &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{keyNode, entry}}, nil
}

// strNode forces the !!str tag so empty defaults render as "" and numeric-looking
// strings stay quoted rather than decoding as numbers.
func strNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func orElse(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

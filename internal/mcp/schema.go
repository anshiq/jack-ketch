package mcp

import "github.com/google/jsonschema-go/jsonschema"

func inputSchema[T any]() *jsonschema.Schema {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	stripNull(s)
	return s
}

func stripNull(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if len(s.Types) > 0 {
		kept := make([]string, 0, len(s.Types))
		for _, t := range s.Types {
			if t != "null" {
				kept = append(kept, t)
			}
		}
		if len(kept) == 1 {
			s.Type, s.Types = kept[0], nil
		} else {
			s.Types = kept
		}
	}
	for _, p := range s.Properties {
		stripNull(p)
	}
	for _, d := range s.Defs {
		stripNull(d)
	}
	stripNull(s.Items)
	stripNull(s.AdditionalProperties)
	for _, sub := range s.AnyOf {
		stripNull(sub)
	}
	for _, sub := range s.OneOf {
		stripNull(sub)
	}
	for _, sub := range s.AllOf {
		stripNull(sub)
	}
}

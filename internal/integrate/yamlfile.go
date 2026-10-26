package integrate

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

func yamlDoc(data []byte) (*yaml.Node, *yaml.Node, error) {
	doc := &yaml.Node{}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := yaml.Unmarshal(data, doc); err != nil {
			return nil, nil, err
		}
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		doc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, errors.New("top level must be a YAML mapping")
	}
	return doc, root, nil
}

func yamlKeyIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func kindTag(k yaml.Kind) string {
	if k == yaml.SequenceNode {
		return "!!seq"
	}
	return "!!map"
}

func yamlChild(m *yaml.Node, key string, kind yaml.Kind, create bool) (*yaml.Node, error) {
	if i := yamlKeyIndex(m, key); i >= 0 {
		v := m.Content[i+1]
		if v.Kind == yaml.ScalarNode && (v.Tag == "!!null" || v.Value == "") {
			v.Kind, v.Tag, v.Value, v.Style = kind, kindTag(kind), "", 0
		}
		if v.Kind != kind {
			return nil, fmt.Errorf("%s has an unexpected type", key)
		}
		return v, nil
	}
	if !create {
		return nil, nil
	}
	v := &yaml.Node{Kind: kind, Tag: kindTag(kind)}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
	return v, nil
}

func yamlEncode(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func yamlSame(a, b *yaml.Node) bool {
	var x, y any
	if a.Decode(&x) != nil || b.Decode(&y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func yamlUpsertMap(data []byte, top, name string, value any) ([]byte, error) {
	doc, root, err := yamlDoc(data)
	if err != nil {
		return nil, err
	}
	m, err := yamlChild(root, top, yaml.MappingNode, true)
	if err != nil {
		return nil, err
	}
	var vn yaml.Node
	if err := vn.Encode(value); err != nil {
		return nil, err
	}
	if i := yamlKeyIndex(m, name); i >= 0 {
		if yamlSame(m.Content[i+1], &vn) {
			return data, nil
		}
		m.Content[i+1] = &vn
	} else {
		m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, &vn)
	}
	return yamlEncode(doc)
}

func yamlRemoveMap(data []byte, top, name string) ([]byte, error) {
	doc, root, err := yamlDoc(data)
	if err != nil {
		return nil, err
	}
	m, err := yamlChild(root, top, yaml.MappingNode, false)
	if err != nil || m == nil {
		return data, err
	}
	i := yamlKeyIndex(m, name)
	if i < 0 {
		return data, nil
	}
	m.Content = append(m.Content[:i], m.Content[i+2:]...)
	return yamlEncode(doc)
}

func listItemIndex(seq *yaml.Node, name string) int {
	for i, item := range seq.Content {
		if item.Kind == yaml.MappingNode {
			if j := yamlKeyIndex(item, "name"); j >= 0 && item.Content[j+1].Value == name {
				return i
			}
		}
	}
	return -1
}

func yamlUpsertList(data []byte, top, name string, value any) ([]byte, error) {
	doc, root, err := yamlDoc(data)
	if err != nil {
		return nil, err
	}
	seq, err := yamlChild(root, top, yaml.SequenceNode, true)
	if err != nil {
		return nil, err
	}
	var vn yaml.Node
	if err := vn.Encode(value); err != nil {
		return nil, err
	}
	if i := listItemIndex(seq, name); i >= 0 {
		if yamlSame(seq.Content[i], &vn) {
			return data, nil
		}
		seq.Content[i] = &vn
	} else {
		seq.Content = append(seq.Content, &vn)
	}
	return yamlEncode(doc)
}

func yamlRemoveList(data []byte, top, name string) ([]byte, error) {
	doc, root, err := yamlDoc(data)
	if err != nil {
		return nil, err
	}
	seq, err := yamlChild(root, top, yaml.SequenceNode, false)
	if err != nil || seq == nil {
		return data, err
	}
	i := listItemIndex(seq, name)
	if i < 0 {
		return data, nil
	}
	seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
	return yamlEncode(doc)
}

type gooseExtension struct {
	Type    string            `yaml:"type"`
	Name    string            `yaml:"name"`
	Enabled bool              `yaml:"enabled"`
	Cmd     string            `yaml:"cmd"`
	Args    []string          `yaml:"args"`
	Envs    map[string]string `yaml:"envs"`
	EnvKeys []string          `yaml:"env_keys"`
	Timeout int               `yaml:"timeout"`
}

func gooseEntry(s ServerSpec) gooseExtension {
	env := s.Env
	if env == nil {
		env = map[string]string{}
	}
	args := s.Args
	if args == nil {
		args = []string{}
	}
	return gooseExtension{Type: "stdio", Name: ServerName, Enabled: true, Cmd: s.Command, Args: args, Envs: env, EnvKeys: []string{}, Timeout: 660}
}

type continueServerEntry struct {
	Name    string            `yaml:"name"`
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env,omitempty"`
}

type continueBlockDoc struct {
	Name       string                `yaml:"name"`
	Version    string                `yaml:"version"`
	Schema     string                `yaml:"schema"`
	MCPServers []continueServerEntry `yaml:"mcpServers"`
}

func continueEntry(s ServerSpec) continueServerEntry {
	args := s.Args
	if args == nil {
		args = []string{}
	}
	return continueServerEntry{Name: ServerName, Command: s.Command, Args: args, Env: s.Env}
}

func continueBlockFile(s ServerSpec) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(continueBlockDoc{Name: ServerName, Version: "0.0.1", Schema: "v1", MCPServers: []continueServerEntry{continueEntry(s)}}); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

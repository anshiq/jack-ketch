package integrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/tailscale/hujson"
)

type jsoncDoc struct {
	root hujson.Value
	unit string
}

func parseJSONC(data []byte) (*jsoncDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}\n")
	}
	v, err := hujson.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("not valid JSON/JSONC: %w", err)
	}
	if _, ok := v.Value.(*hujson.Object); !ok {
		return nil, errors.New("top level must be a JSON object")
	}
	return &jsoncDoc{root: v, unit: indentUnit(data)}, nil
}

func indentUnit(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || len(trimmed) == len(line) {
			continue
		}
		lead := line[:len(line)-len(trimmed)]
		if strings.HasPrefix(lead, "\t") {
			return "\t"
		}
		if len(lead) >= 2 && len(lead) <= 8 {
			return lead
		}
	}
	return "  "
}

func (d *jsoncDoc) bytes() []byte { return d.root.Pack() }

func (d *jsoncDoc) rootObject() *hujson.Object { return d.root.Value.(*hujson.Object) }

func memberIndex(obj *hujson.Object, name string) int {
	for i := range obj.Members {
		if lit, ok := obj.Members[i].Name.Value.(hujson.Literal); ok && lit.String() == name {
			return i
		}
	}
	return -1
}

func (d *jsoncDoc) walk(path []string) (*hujson.Object, int, error) {
	obj := d.rootObject()
	for depth, key := range path {
		i := memberIndex(obj, key)
		if i < 0 {
			return obj, depth, nil
		}
		child, ok := obj.Members[i].Value.Value.(*hujson.Object)
		if !ok {
			return nil, 0, fmt.Errorf("%q is not a JSON object", strings.Join(path[:depth+1], "."))
		}
		obj = child
	}
	return obj, len(path), nil
}

func (d *jsoncDoc) get(path []string, name string) (json.RawMessage, bool, error) {
	obj, depth, err := d.walk(path)
	if err != nil || depth < len(path) {
		return nil, false, err
	}
	i := memberIndex(obj, name)
	if i < 0 {
		return nil, false, nil
	}
	std, err := hujson.Standardize(obj.Members[i].Value.Pack())
	if err != nil {
		return nil, false, err
	}
	return json.RawMessage(std), true, nil
}

func (d *jsoncDoc) upsert(path []string, name string, value any) (bool, error) {
	obj, depth, err := d.walk(path)
	if err != nil {
		return false, err
	}
	if depth < len(path) {
		var inner any = orderedObject{{name, value}}
		for k := len(path) - 1; k > depth; k-- {
			inner = orderedObject{{path[k], inner}}
		}
		return true, d.appendMember(obj, path[depth], inner, depth+1)
	}
	if i := memberIndex(obj, name); i >= 0 {
		same, err := sameJSON(obj.Members[i].Value, value)
		if err != nil {
			return false, err
		}
		if same {
			return false, nil
		}
		return true, d.replaceMember(obj, i, value, depth+1)
	}
	return true, d.appendMember(obj, name, value, depth+1)
}

func dropMember(obj *hujson.Object, i int) {
	wasLast := i == len(obj.Members)-1
	obj.Members = append(obj.Members[:i], obj.Members[i+1:]...)
	if wasLast && len(obj.Members) > 0 {
		obj.Members[len(obj.Members)-1].Value.AfterExtra = nil
	}
}

func (d *jsoncDoc) remove(path []string, name string) (bool, error) {
	obj, depth, err := d.walk(path)
	if err != nil || depth < len(path) {
		return false, err
	}
	i := memberIndex(obj, name)
	if i < 0 {
		return false, nil
	}
	dropMember(obj, i)
	if len(obj.Members) == 0 && len(path) > 0 {
		if parent, pdepth, err := d.walk(path[:len(path)-1]); err == nil && pdepth == len(path)-1 {
			if j := memberIndex(parent, path[len(path)-1]); j >= 0 {
				dropMember(parent, j)
			}
		}
	}
	return true, nil
}

func lineIndent(extra hujson.Extra) (string, bool) {
	s := string(extra)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:], true
	}
	return "", false
}

func (d *jsoncDoc) memberIndent(obj *hujson.Object, depth int) string {
	if len(obj.Members) > 0 {
		if ind, ok := lineIndent(obj.Members[0].Name.BeforeExtra); ok {
			return ind
		}
	}
	return strings.Repeat(d.unit, depth)
}

func (d *jsoncDoc) appendMember(obj *hujson.Object, name string, value any, depth int) error {
	indent := d.memberIndent(obj, depth)
	data, err := json.MarshalIndent(value, indent, d.unit)
	if err != nil {
		return err
	}
	key, err := json.Marshal(name)
	if err != nil {
		return err
	}
	pair, err := hujson.Parse([]byte("{" + string(key) + ": " + string(data) + "}"))
	if err != nil {
		return err
	}
	m := pair.Value.(*hujson.Object).Members[0]
	m.Name.BeforeExtra = hujson.Extra("\n" + indent)
	m.Value.BeforeExtra = hujson.Extra(" ")
	m.Value.AfterExtra = nil
	if len(obj.Members) == 0 {
		closing := ""
		if len(indent) >= len(d.unit) {
			closing = indent[:len(indent)-len(d.unit)]
		}
		obj.AfterExtra = hujson.Extra("\n" + closing)
	}
	obj.Members = append(obj.Members, m)
	return nil
}

func (d *jsoncDoc) replaceMember(obj *hujson.Object, i int, value any, depth int) error {
	indent, ok := lineIndent(obj.Members[i].Name.BeforeExtra)
	if !ok {
		indent = d.memberIndent(obj, depth)
	}
	data, err := json.MarshalIndent(value, indent, d.unit)
	if err != nil {
		return err
	}
	v, err := hujson.Parse(data)
	if err != nil {
		return err
	}
	v.BeforeExtra = obj.Members[i].Value.BeforeExtra
	v.AfterExtra = obj.Members[i].Value.AfterExtra
	obj.Members[i].Value = v
	return nil
}

func sameJSON(v hujson.Value, want any) (bool, error) {
	std, err := hujson.Standardize(v.Pack())
	if err != nil {
		return false, err
	}
	var a, b any
	if err := json.Unmarshal(std, &a); err != nil {
		return false, err
	}
	wb, err := json.Marshal(want)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(wb, &b); err != nil {
		return false, err
	}
	return reflect.DeepEqual(a, b), nil
}

package config

import (
	"embed"
)

//go:embed schema/jack-ketch.v3.json
var v3SchemaFS embed.FS

func V3JSONSchemaRaw() []byte {
	data, err := v3SchemaFS.ReadFile("schema/jack-ketch.v3.json")
	if err != nil {
		panic("config: embedded schema missing: " + err.Error())
	}
	return data
}

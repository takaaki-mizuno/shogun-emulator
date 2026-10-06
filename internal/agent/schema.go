package agent

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

// Schema は JSON Schema の 1 つのノード。
//
// プロパティを定義の順に書き出すため、map を使わず並びで持つ。
// MCP のツール定義と CLI のヘルプが、構造体の並びのとおりに引数を示す
// ためである。
type Schema struct {
	Type        string
	Description string
	Default     string
	Items       *Schema
	Properties  []Property
	Required    []string
}

// Property は object の 1 つのプロパティ。
type Property struct {
	Name   string
	Schema *Schema
}

// MarshalJSON はプロパティを定義の順に書き出す。
func (s *Schema) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	field := func(name string, v any) error {
		if !first {
			b.WriteByte(',')
		}
		first = false
		k, _ := json.Marshal(name)
		b.Write(k)
		b.WriteByte(':')
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		b.Write(data)
		return nil
	}
	if s.Type != "" {
		if err := field("type", s.Type); err != nil {
			return nil, err
		}
	}
	if s.Description != "" {
		if err := field("description", s.Description); err != nil {
			return nil, err
		}
	}
	if s.Default != "" {
		var v any
		if json.Unmarshal([]byte(s.Default), &v) != nil {
			v = s.Default
		}
		if err := field("default", v); err != nil {
			return nil, err
		}
	}
	if s.Items != nil {
		if err := field("items", s.Items); err != nil {
			return nil, err
		}
	}
	if s.Type == "object" {
		var p bytes.Buffer
		p.WriteByte('{')
		for i, prop := range s.Properties {
			if i > 0 {
				p.WriteByte(',')
			}
			k, _ := json.Marshal(prop.Name)
			p.Write(k)
			p.WriteByte(':')
			data, err := json.Marshal(prop.Schema)
			if err != nil {
				return nil, err
			}
			p.Write(data)
		}
		p.WriteByte('}')
		if err := field("properties", json.RawMessage(p.Bytes())); err != nil {
			return nil, err
		}
		if len(s.Required) > 0 {
			if err := field("required", s.Required); err != nil {
				return nil, err
			}
		}
		if err := field("additionalProperties", false); err != nil {
			return nil, err
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// rawMessageType は任意の JSON を受け取るフィールドの型。
var rawMessageType = reflect.TypeOf(json.RawMessage(nil))

// SchemaOf は値の型から JSON Schema を作る。nil のときは引数を持たない
// object を返す。
//
// フィールドのタグ json・desc・default を読む。json タグに omitempty が
// 無いフィールドを必須とする。埋め込んだ構造体のフィールドは、外側の
// 構造体のフィールドとして並べる（encoding/json と同じ扱い）。
func SchemaOf(v any) *Schema {
	if v == nil {
		return &Schema{Type: "object"}
	}
	return schemaOfType(reflect.TypeOf(v))
}

func schemaOfType(t reflect.Type) *Schema {
	if t == rawMessageType {
		return &Schema{}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return schemaOfType(t.Elem())
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Schema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Slice, reflect.Array:
		return &Schema{Type: "array", Items: schemaOfType(t.Elem())}
	case reflect.Struct:
		s := &Schema{Type: "object"}
		addFields(s, t)
		return s
	}
	// interface など。任意の値を受け取る。
	return &Schema{}
}

// addFields は構造体のフィールドをプロパティとして加える。
func addFields(s *Schema, t reflect.Type) {
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			addFields(s, f.Type)
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		ps := schemaOfType(f.Type)
		ps.Description = f.Tag.Get("desc")
		ps.Default = f.Tag.Get("default")
		s.Properties = append(s.Properties, Property{Name: name, Schema: ps})
		if !strings.Contains(opts, "omitempty") {
			s.Required = append(s.Required, name)
		}
	}
}

// PropertyNames は object のプロパティの名前を定義の順に返す。
func (s *Schema) PropertyNames() []string {
	out := make([]string, len(s.Properties))
	for i, p := range s.Properties {
		out[i] = p.Name
	}
	return out
}

// Property は名前でプロパティを引く。
func (s *Schema) Property(name string) (*Schema, bool) {
	for _, p := range s.Properties {
		if p.Name == name {
			return p.Schema, true
		}
	}
	return nil, false
}

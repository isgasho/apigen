package apigen

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"

	"github.com/jinzhu/inflection"
	"github.com/morikuni/failure"
)

type jsonType int

const (
	jsonTypeNull jsonType = iota
	jsonTypeBool
	jsonTypeNumber
	jsonTypeString
	jsonTypeArray
	jsonTypeObject
)

type Decoder interface {
	Decode(io.Reader) (*structType, error)
}

type JSONDecoder struct{}

func (d *JSONDecoder) Decode(r io.Reader) (*structType, error) {
	v := make(map[string]interface{})
	if err := json.NewDecoder(r).Decode(&v); err != nil {
		return nil, failure.Wrap(err)
	}

	return decodeJSONObject(v), nil
}

func decodeJSONType(v interface{}) _type {
	switch detectJSONType(v) {
	case jsonTypeNull:
		return emptyIfaceType
	case jsonTypeObject:
		return decodeJSONObject(v.(map[string]interface{}))
	case jsonTypeArray:
		return decodeJSONArray(v.([]interface{}))
	case jsonTypeBool:
		return typeBool
	case jsonTypeString:
		return typeString
	case jsonTypeNumber:
		return typeFloat64
	default:
		panic("unreachable")
	}
}

func decodeJSONObject(o map[string]interface{}) *structType {
	var s structType
	for k, v := range o {
		key := public(k)
		field := &structField{
			name: key,
			tags: map[string][]string{"json": {k, "omitempty"}},
		}

		switch detectJSONType(v) {
		case jsonTypeNull:
			field._type = emptyIfaceType
		case jsonTypeObject:
			field._type = &definedType{
				name:    key, // Type name is same as the field name.
				pointer: true,
				_type:   decodeJSONObject(v.(map[string]interface{})),
			}
		case jsonTypeArray:
			t := decodeJSONArray(v.([]interface{}))
			if t.elemType == emptyIfaceType {
				field._type = t
			} else {
				field._type = &sliceType{
					// Element type name is singular name of field name.
					elemType: &definedType{
						name:    inflection.Singular(key),
						pointer: true,
						_type:   t.elemType,
					},
				}
			}
		case jsonTypeBool:
			field._type = typeBool
		case jsonTypeString:
			field._type = typeString
		case jsonTypeNumber:
			field._type = typeFloat64
		}

		s.fields = append(s.fields, field)
	}

	sort.Slice(s.fields, func(i, j int) bool {
		return s.fields[i].name < s.fields[j].name
	})

	return &s
}

func decodeJSONArray(arr []interface{}) *sliceType {
	if len(arr) == 0 {
		return &sliceType{elemType: emptyIfaceType}
	}

	return &sliceType{elemType: decodeJSONType(arr[0])}
}

func detectJSONType(v interface{}) jsonType {
	switch cv := v.(type) {
	case nil:
		return jsonTypeNull
	case map[string]interface{}:
		return jsonTypeObject
	case []interface{}:
		return jsonTypeArray
	case bool:
		return jsonTypeBool
	case string:
		return jsonTypeString
	case float64:
		return jsonTypeNumber
	default:
		panic(fmt.Sprintf("unknown type: %T", cv))
	}
}

func structFromQuery(q url.Values) *structType {
	var s structType
	for k, v := range q {
		field := &structField{
			name: public(k),
			tags: map[string][]string{"name": {k}}}
		if len(v) == 1 {
			field._type = typeString
		} else {
			field._type = &sliceType{elemType: typeString}
		}
		s.fields = append(s.fields, field)
	}
	return &s
}

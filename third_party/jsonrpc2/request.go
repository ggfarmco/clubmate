package jsonrpc2

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strconv"
)

// Request represents a JSON-RPC request or
// notification. See
// http://www.jsonrpc.org/specification#request_object and
// http://www.jsonrpc.org/specification#notification.
type Request struct {
	Method string           `json:"method"`
	Params *json.RawMessage `json:"params,omitempty"`
	ID     ID               `json:"id"`
	Notif  bool             `json:"-"`

	// Meta optionally provides metadata to include in the request.
	//
	// NOTE: It is not part of spec. However, it is useful for propagating
	// tracing context, etc.
	Meta *json.RawMessage `json:"meta,omitempty"`

	// ExtraFields optionally adds fields to the root of the JSON-RPC request.
	//
	// NOTE: It is not part of the spec, but there are other protocols based on
	// JSON-RPC 2 that require it.
	ExtraFields []RequestField `json:"-"`
}

// MarshalJSON implements json.Marshaler and adds the "jsonrpc":"2.0"
// property.
func (r Request) MarshalJSON() ([]byte, error) {
	return r.appendJSON(nil)
}

func (r *Request) appendJSON(dst []byte) ([]byte, error) {
	dst = slices.Grow(dst, 64+len(r.Method)+rawLen(r.Params)+rawLen(r.Meta))
	dst = append(dst, '{')
	if !r.Notif {
		dst = append(dst, `"id":`...)
		dst = r.ID.appendJSON(dst)
		dst = append(dst, ',')
	}
	dst = append(dst, `"jsonrpc":"2.0"`...)
	var err error
	if dst, err = appendField(dst, "meta", r.Meta); err != nil {
		return nil, err
	}
	dst = append(dst, `,"method":`...)
	dst, _ = jsontext.AppendQuote(dst, r.Method)
	if dst, err = appendField(dst, "params", r.Params); err != nil {
		return nil, err
	}
	for _, field := range r.ExtraFields {
		if reservedField(field.Name) {
			continue
		}
		b, err := json.Marshal(field.Value)
		if err != nil {
			return nil, err
		}
		dst = append(dst, ',')
		dst, _ = jsontext.AppendQuote(dst, field.Name)
		dst = append(dst, ':')
		dst = append(dst, b...)
	}
	return append(dst, '}'), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *Request) UnmarshalJSON(data []byte) error {
	*r = Request{Notif: true}
	var hasMethod bool
	err := eachField(data, func(name string, value jsontext.Value) error {
		switch name {
		case "method":
			if value.Kind() != '"' {
				return errors.New("missing method field")
			}
			method, err := unquote(value)
			if err != nil {
				return err
			}
			r.Method, hasMethod = method, true
		case "params":
			r.Params = cloneRaw(value)
		case "meta":
			r.Meta = cloneRaw(value)
		case "id":
			return r.setID(value)
		case "jsonrpc":
		default:
			var v interface{}
			dec := json.NewDecoder(bytes.NewReader(value))
			dec.UseNumber()
			if err := dec.Decode(&v); err != nil {
				return err
			}
			r.ExtraFields = append(r.ExtraFields, RequestField{Name: name, Value: v})
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !hasMethod {
		return errors.New("missing method field")
	}
	return nil
}

func (r *Request) setID(value jsontext.Value) error {
	switch value.Kind() {
	case 'n':
		r.ID, r.Notif = ID{}, true
	case '"':
		s, err := unquote(value)
		if err != nil {
			return err
		}
		r.ID, r.Notif = ID{Str: s, IsString: true}, false
	case '0':
		id, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil {
			return fmt.Errorf("failed to unmarshal ID: %w", err)
		}
		r.ID, r.Notif = ID{Num: uint64(id)}, false
	default:
		return fmt.Errorf("unexpected ID type: %s", value.Kind())
	}
	return nil
}

func reservedField(name string) bool {
	switch name {
	case "id", "jsonrpc", "meta", "method", "params":
		return true
	}
	return false
}

// SetParams sets r.Params to the JSON encoding of v. If JSON
// marshaling fails, it returns an error.
func (r *Request) SetParams(v interface{}) error {
	b, err := marshalValue(v)
	if err != nil {
		return err
	}
	r.Params = (*json.RawMessage)(&b)
	return nil
}

// SetMeta sets r.Meta to the JSON encoding of v. If JSON
// marshaling fails, it returns an error.
func (r *Request) SetMeta(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r.Meta = (*json.RawMessage)(&b)
	return nil
}

// SetExtraField adds an entry to r.ExtraFields, so that it is added to the
// JSON encoding of the request, as a way to add arbitrary extensions to
// JSON RPC 2.0. If JSON marshaling fails, it returns an error.
func (r *Request) SetExtraField(name string, v interface{}) error {
	if reservedField(name) {
		return fmt.Errorf("invalid extra field %q", name)
	}
	r.ExtraFields = append(r.ExtraFields, RequestField{
		Name:  name,
		Value: v,
	})
	return nil
}

// RequestField is a top-level field that can be added to the JSON-RPC request.
type RequestField struct {
	Name  string
	Value interface{}
}

package jsonrpc2

import (
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"slices"
)

// Response represents a JSON-RPC response. See
// http://www.jsonrpc.org/specification#response_object.
type Response struct {
	ID     ID               `json:"id"`
	Result *json.RawMessage `json:"result,omitempty"`
	Error  *Error           `json:"error,omitempty"`

	// Meta optionally provides metadata to include in the response.
	//
	// NOTE: It is not part of spec. However, it is useful for propagating
	// tracing context, etc.
	Meta *json.RawMessage `json:"meta,omitempty"`

	// SPEC NOTE: The spec says "If there was an error in detecting
	// the id in the Request object (e.g. Parse error/Invalid
	// Request), it MUST be Null." If we made the ID field nullable,
	// then we'd have to make it a pointer type. For simplicity, we're
	// ignoring the case where there was an error in detecting the ID
	// in the Request object.
}

// MarshalJSON implements json.Marshaler and adds the "jsonrpc":"2.0"
// property.
func (r Response) MarshalJSON() ([]byte, error) {
	return r.appendJSON(nil)
}

func (r *Response) appendJSON(dst []byte) ([]byte, error) {
	if (r.Result == nil || len(*r.Result) == 0) && r.Error == nil {
		return nil, errors.New("can't marshal *jsonrpc2.Response (must have result or error)")
	}
	dst = slices.Grow(dst, 64+rawLen(r.Result)+rawLen(r.Meta))
	dst = append(dst, `{"id":`...)
	dst = r.ID.appendJSON(dst)
	var err error
	if r.Result != nil && len(*r.Result) > 0 {
		if dst, err = appendField(dst, "result", r.Result); err != nil {
			return nil, err
		}
	}
	if r.Error != nil {
		b, err := json.Marshal(r.Error)
		if err != nil {
			return nil, err
		}
		dst = append(dst, `,"error":`...)
		dst = append(dst, b...)
	}
	if r.Meta != nil && len(*r.Meta) > 0 {
		if dst, err = appendField(dst, "meta", r.Meta); err != nil {
			return nil, err
		}
	}
	return append(dst, `,"jsonrpc":"2.0"}`...), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *Response) UnmarshalJSON(data []byte) error {
	*r = Response{}
	return eachField(data, func(name string, value jsontext.Value) error {
		switch name {
		case "id":
			return r.ID.UnmarshalJSON(value)
		case "result":
			r.Result = cloneRaw(value)
		case "error":
			if value.Kind() == 'n' {
				r.Error = nil
				return nil
			}
			r.Error = new(Error)
			return json.Unmarshal(value, r.Error)
		case "meta":
			if value.Kind() == 'n' {
				r.Meta = nil
				return nil
			}
			r.Meta = cloneRaw(value)
		}
		return nil
	})
}

// SetResult sets r.Result to the JSON representation of v. If JSON
// marshaling fails, it returns an error.
func (r *Response) SetResult(v interface{}) error {
	b, err := marshalValue(v)
	if err != nil {
		return err
	}
	r.Result = (*json.RawMessage)(&b)
	return nil
}

package systemone

import (
	"bytes"
	"encoding/json"
	"errors"
	"iter"
)

// Ordered is a JSON object that keeps its keys in insertion (or source) order.
// The zero value is ready to use.
type Ordered[V any] struct {
	keys []string
	vals map[string]V
}

// Set adds or replaces key. A new key goes last.
func (o *Ordered[V]) Set(key string, v V) {
	if o.vals == nil {
		o.vals = map[string]V{}
	}
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

// Get returns the value for key.
func (o *Ordered[V]) Get(key string) (V, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// Len is the number of keys.
func (o *Ordered[V]) Len() int { return len(o.keys) }

// All yields keys and values in order.
func (o *Ordered[V]) All() iter.Seq2[string, V] {
	return func(yield func(string, V) bool) {
		for _, k := range o.keys {
			if !yield(k, o.vals[k]) {
				return
			}
		}
	}
}

// MarshalJSON writes the object with its keys in order.
func (o Ordered[V]) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		vb, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON reads an object, keeping its keys in source order. A repeated
// key keeps its first position and its last value.
func (o *Ordered[V]) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("systemone: expected a JSON object")
	}
	*o = Ordered[V]{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return errors.New("systemone: expected an object key")
		}
		var v V
		if err := dec.Decode(&v); err != nil {
			return err
		}
		o.Set(key, v)
	}
	_, err = dec.Token()
	return err
}

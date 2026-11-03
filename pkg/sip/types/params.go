package types

import (
	"io"
	"strings"
)

// Params is a map of string with extended functions
type Params map[string]string

// Set sets the value for the given key
func (p Params) Set(key, value string) {
	p[key] = value
}

// Get returns the value for the given key
func (p Params) Get(key string) (string, bool) {
	s, ok := p[key]

	return s, ok
}

// Del removes the given key
func (p Params) Del(key string) {
	delete(p, key)
}

// Length returns the number of items
func (p Params) Length() int {
	return len(p)
}

// Clone returns a copy of the params
func (p Params) Clone() Params {
	cp := make(Params, len(p))

	for k, v := range p {
		cp[k] = v
	}

	return cp
}

// HasKey returns true if the key exists
func (p Params) HasKey(key string) bool {
	_, ok := p[key]
	return ok
}

// Keys returns the keys
func (p Params) Keys() []string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	return keys
}

func (p Params) WriteTo(sep byte, buffer io.StringWriter) {
	first := true
	for k, v := range p {
		if !first {
			buffer.WriteString(string(sep))
		}
		buffer.WriteString(k)

		if v != "" {
			buffer.WriteString("=")
			buffer.WriteString(v)
		}
		first = false
	}
}

// ToString converts Params to string
func (p Params) ToString(sep byte) string {
	var buffer strings.Builder
	p.WriteTo(sep, &buffer)

	return buffer.String()
}

// String implements the stringer interface to print out the params.
// The output will be printed in the form of key1=value1;key2=value2.
// To change the separator, use ToString(sep byte) method.
func (p Params) String() string {
	return p.ToString(';')
}

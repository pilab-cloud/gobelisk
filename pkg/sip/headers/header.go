package headers

import (
	"fmt"
	"io"
)

// Header is a single SIP header.
type Header interface {
	// HeaderBase is a base for all headers
	HeaderBase

	// Name returns header name.
	Name() string

	// Value returns header value, or multiple values separated by comma
	Value() string

	// String returns string representation of the header in RFC 3261 form.
	String() string

	// StringWrite is same as String but lets you to provide writter and reduce allocations
	Write(w io.Writer)

	// Clone returns a deep copy of the header
	Clone() Header
}

// HeaderBase is a base for all headers
type HeaderBase interface {
	Name() string
	Value() string
}

// headerWriter implements Header
type headerWriter struct {
	HeaderBase
}

// Write writes out a header.
func (hw *headerWriter) Write(w io.Writer) {
	_, _ = fmt.Fprintf(w, "%s: %s", hw.Name(), hw.Value())

	// return err
}

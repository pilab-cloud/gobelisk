package headers

import (
	"fmt"
	"io"
	"strings"
)

// CallIDHeader is a Call-ID header presentation
type CallIDHeader string

// String returns a string representation of the header
func (h *CallIDHeader) String() string {
	var buffer strings.Builder
	h.Write(&buffer)

	return buffer.String()
}

// Name returns the name of the header
func (h *CallIDHeader) Name() string { return "Call-ID" }

// Value returns the value of the header
func (h *CallIDHeader) Value() string { return string(*h) }

// Clone clones the header
func (h *CallIDHeader) Clone() Header {
	var newCallID CallIDHeader
	newCallID = *h

	return &newCallID
}

// Write writes the header
func (h *CallIDHeader) Write(io io.Writer) {
	fmt.Fprintf(io, "%s: %s", h.Name(), h.Value())
}

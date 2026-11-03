package headers

import (
	"fmt"
	"io"
	"strings"
)

// NewHeader creates generic type of header
func NewHeader(name string, value ...string) Header {
	return &genericHeader{
		HeaderName: name,
		Contents:   value,
	}
}

// genericHeader is generic struct for unknown headers
type genericHeader struct {
	// The name of the header.
	HeaderName string

	// The contents of the header
	Contents []string
}

func (h *genericHeader) String() string {
	var buffer strings.Builder
	h.Write(&buffer)
	return buffer.String()
}

func (h *genericHeader) Write(w io.Writer) {
	fmt.Fprintf(w, "%s: %s", h.Name(), h.Value())
}

// Name returns header name
func (h *genericHeader) Name() string {
	return h.HeaderName
}

// Value returns header value
func (h *genericHeader) Value() string {
	return strings.Join(h.Contents, ", ")
}

func (h *genericHeader) Clone() Header {
	if h == nil {
		var newHeader *genericHeader
		return newHeader
	}

	return &genericHeader{
		HeaderName: h.HeaderName,
		Contents:   h.Contents,
	}
}

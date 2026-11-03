package headers

import (
	"fmt"
	"io"
	"strings"
)

// ContentTypeHeader  is Content-Type header representation.
type ContentTypeHeader string

func (h *ContentTypeHeader) String() string {
	var buffer strings.Builder
	h.Write(&buffer)
	return buffer.String()
}

func (h *ContentTypeHeader) Write(w io.Writer) {
	fmt.Fprintf(w, "%s: %s", h.Name(), h.Value())
}

func (h *ContentTypeHeader) Name() string { return "Content-Type" }

func (h *ContentTypeHeader) Value() string { return string(*h) }

func (h *ContentTypeHeader) Clone() Header {
	var newHeader ContentTypeHeader
	return &newHeader
}

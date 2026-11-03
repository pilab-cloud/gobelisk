package headers

import (
	"fmt"
	"io"
	"strconv"
)

// ExpiresHeader is Expires header representation
type ExpiresHeader uint32

// String returns string representation of Expires header
func (h *ExpiresHeader) String() string {
	return fmt.Sprintf("%s: %s", h.Name(), h.Value())
}

// Write writes Expires header to writer
func (h *ExpiresHeader) Write(w io.Writer) {
	fmt.Fprintf(w, "%s: %s", h.Name(), h.Value())
}

func (h *ExpiresHeader) Name() string { return "Expires" }

func (h ExpiresHeader) Value() string { return strconv.Itoa(int(h)) }

func (h *ExpiresHeader) Clone() Header {
	var newExp ExpiresHeader
	newExp = *h
	return &newExp
}

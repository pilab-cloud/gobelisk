package headers

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
)

// MaxForwardsHeader is Max-Forwards header representation
type MaxForwardsHeader uint32

// Write writes Max-Forwards header to writer
func (h *MaxForwardsHeader) String() string {
	var buf bytes.Buffer
	h.Write(&buf)

	return buf.String()
}

func (h *MaxForwardsHeader) Name() string { return "Max-Forwards" }

func (h *MaxForwardsHeader) Value() string { return strconv.Itoa(int(*h)) }

func (h *MaxForwardsHeader) Clone() Header { return h }

func (h *MaxForwardsHeader) Dec() {
	*h = MaxForwardsHeader(uint32(*h) - 1)
}

func (h MaxForwardsHeader) Val() uint32 {
	return uint32(h)
}

// Write writes Max-Forwards header to writer
func (h *MaxForwardsHeader) Write(w io.Writer) {
	fmt.Fprintf(w, "%s: %s", h.Name(), h.Value())
}

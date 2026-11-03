package headers

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/sip/types"
)

// RecordRouteHeader is Record-Route header representation.
type RecordRouteHeader struct {
	Address types.Address
	Next    *RecordRouteHeader
}

func (h *RecordRouteHeader) Name() string { return "Record-Route" }

func (h *RecordRouteHeader) Value() string {
	var buffer bytes.Buffer
	h.ValueWrite(&buffer)
	return buffer.String()
}

func (h *RecordRouteHeader) ValueWrite(buffer io.StringWriter) {
	for hop := h; hop != nil; hop = hop.Next {
		buffer.WriteString("<")
		buffer.WriteString(hop.Address.Addr())
		buffer.WriteString(">")

		if hop.Next != nil {
			buffer.WriteString(", ")
		}
	}
}

func (h *RecordRouteHeader) String() string {
	var buffer strings.Builder
	h.Write(&buffer)
	return buffer.String()
}

func (h *RecordRouteHeader) Write(w io.Writer) {
	fmt.Fprintf(w, "%s: %s", h.Name(), h.Value())
}

func (h *RecordRouteHeader) Clone() *RecordRouteHeader {
	newRoute := h.cloneFirst()
	newNext := newRoute
	for hop := h.Next; hop != nil; hop = hop.Next {
		newNext.Next = hop.Next.cloneFirst()
		newNext = newNext.Next
	}

	return newRoute
}

package headers

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/sip/types"
)

// RouteHeader  is Route header representation.
type RouteHeader struct {
	Address types.Address
}

func (h *RouteHeader) Name() string { return "Route" }

func (h *RouteHeader) Value() string {
	var buffer bytes.Buffer
	h.ValueWrite(&buffer)
	return buffer.String()
}

func (h *RouteHeader) ValueWrite(buffer io.StringWriter) {
	buffer.WriteString("<")
	buffer.WriteString(h.Address.Addr())
	buffer.WriteString(">")
}

func (h *RouteHeader) String() string {
	var buffer strings.Builder
	h.Write(&buffer)
	return buffer.String()
}

func (h *RouteHeader) Write(w io.Writer) {
	fmt.Fprintf(w, "%s: %s", h.Name(), h.Value())
}

func (h *RouteHeader) Clone() *RouteHeader {
	var newRoute *RouteHeader
	newRoute = &RouteHeader{
		Address: h.Address,
	}
	return newRoute
}

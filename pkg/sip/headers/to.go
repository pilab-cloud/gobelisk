package headers

import (
	"io"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/sip/types"
)

// ToHeader introduces SIP 'To' header
type ToHeader struct {
	headerWriter

	// The display name from the header, may be omitted.
	DisplayName string
	Address     types.Address
	// Any parameters present in the header.
	Params types.Params
}

func (h *ToHeader) String() string {
	var buffer strings.Builder
	h.Write(&buffer)
	return buffer.String()
}

func (h *ToHeader) Name() string { return "To" }

func (h *ToHeader) Value() string {
	var buffer strings.Builder
	h.ValueWrite(&buffer)
	return buffer.String()
}

func (h *ToHeader) ValueWrite(buffer io.StringWriter) {
	if h.DisplayName != "" {
		buffer.WriteString("\"")
		buffer.WriteString(h.DisplayName)
		buffer.WriteString("\" ")
	}

	// buffer.WriteString(fmt.Sprintf("<%s>", h.Address))
	buffer.WriteString("<")
	buffer.WriteString(h.Address.Addr())
	// buffer.WriteString(h.Address.String())
	buffer.WriteString(">")

	if h.Params != nil && h.Params.Length() > 0 {
		buffer.WriteString(";")
		buffer.WriteString(h.Params.ToString(';'))
	}
}

func (header *ToHeader) Next() Header {
	return nil
}

// Copy the header.
func (h *ToHeader) Clone() Header {
	if h == nil {
		return new(ToHeader)
	}

	newTo := &ToHeader{
		DisplayName: h.DisplayName,
		Address:     h.Address,
	}
	// if h.Address != nil {
	// 	newTo.Address = h.Address.Clone()
	// }
	if h.Params != nil {
		newTo.Params = h.Params.Clone()
	}

	return newTo
}

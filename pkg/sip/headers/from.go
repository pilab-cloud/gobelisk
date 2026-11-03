package headers

import (
	"io"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/sip/types"
)

type FromHeader struct {
	headerWriter

	// The display name from the header, may be omitted.
	DisplayName string

	// The address from the header.
	Address types.Address

	// Any parameters present in the header.
	Params types.Params
}

func (h *FromHeader) String() string {
	var buffer strings.Builder
	h.Write(&buffer)
	return buffer.String()
}

func (h *FromHeader) Name() string { return "From" }

func (h *FromHeader) Value() string {
	var buffer strings.Builder
	h.ValueWrite(&buffer)
	return buffer.String()
}

func (h *FromHeader) ValueWrite(buffer io.StringWriter) {
	if h.DisplayName != "" {
		buffer.WriteString("\"")
		buffer.WriteString(h.DisplayName)
		buffer.WriteString("\" ")
	}

	buffer.WriteString("<")
	buffer.WriteString(h.Address.Addr())
	buffer.WriteString(">")

	if h.Params != nil && h.Params.Length() > 0 {
		buffer.WriteString(";")
		buffer.WriteString(h.Params.ToString(';'))
	}
}

func (h *FromHeader) Clone() Header {
	if h == nil {
		return new(FromHeader)
	}

	newFrom := &FromHeader{
		DisplayName: h.DisplayName,
		Address:     h.Address,
	}

	// if h.Address != nil {
	// 	newFrom.Address = h.Address.Clone()
	// }

	if h.Params != nil {
		newFrom.Params = h.Params.Clone()
	}

	return newFrom
}

func (header *FromHeader) Next() Header {
	return nil
}

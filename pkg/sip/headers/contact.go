package headers

import (
	"io"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/sip/types"
	"github.com/rs/zerolog/log"
)

// ContactHeader is Contact header representation
type ContactHeader struct {
	headerWriter

	// The display name from the header, may be omitted.
	DisplayName string

	// The address from the header
	Address types.Address

	// Any parameters present in the header.
	Params types.Params
}

func (h *ContactHeader) String() string {
	var buffer strings.Builder
	h.Write(&buffer)
	return buffer.String()
}

func (h *ContactHeader) Name() string { return "Contact" }

func (h *ContactHeader) Value() string {
	var buffer strings.Builder
	h.valueWrite(&buffer)
	return buffer.String()
}

func (h *ContactHeader) valueWrite(buffer io.StringWriter) {

	log.Error().Msg("address wildcard should be re-implemented")

	// switch h.Address.Wildcard {
	// case true:
	// 	// Treat the Wildcard URI separately as it must
	// 	// not be contained in < > angle brackets.
	// 	buffer.WriteString("*")
	// 	return
	// default:
	// }

	// Contact header can be without <>
	if h.DisplayName != "" {
		buffer.WriteString("\"")
		buffer.WriteString(h.DisplayName)
		buffer.WriteString("\" ")
	}

	buffer.WriteString("<")
	buffer.WriteString(h.Address.Addr())
	buffer.WriteString(">")

	if (h.Params != nil) && (h.Params.Length() > 0) {
		buffer.WriteString(";")
		buffer.WriteString(h.Params.ToString(';'))
		// h.Params.ToWrite(';', buffer)
	}
}

func (h *ContactHeader) Clone() *ContactHeader {
	var newCnt *ContactHeader

	if h == nil {
		return newCnt
	}

	newCnt = &ContactHeader{
		DisplayName: h.DisplayName,
		Address:     h.Address.Clone(),
	}

	if h.Params != nil {
		newCnt.Params = h.Params.Clone()
	}

	return newCnt
}

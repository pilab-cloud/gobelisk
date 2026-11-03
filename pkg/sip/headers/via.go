package headers

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/sip/types"
)

// ViaHeader is Via header representation.
// It can be linked list of multiple via if they are part of one header
type ViaHeader struct {
	headerWriter

	// Protocol name. E.g. 'SIP'.
	ProtocolName string

	// Protocol version. E.g. '2.0'.
	ProtocolVersion string

	// Transport. E.g. 'UDP'
	Transport string

	// TODO consider changing Host Port as struct Addr from transport
	// Host
	Host string

	// Port
	Port int // This is optional

	// Params
	Params types.Params
}

func (hop *ViaHeader) SentBy() string {
	var buf bytes.Buffer
	buf.WriteString(hop.Host)
	if hop.Port > 0 {
		buf.WriteString(fmt.Sprintf(":%d", hop.Port))
	}

	return buf.String()
}

func (h *ViaHeader) String() string {
	var buffer strings.Builder
	h.Write(&buffer)

	return buffer.String()
}

func (h *ViaHeader) Name() string { return "Via" }

func (h *ViaHeader) Value() string {
	var buffer bytes.Buffer
	h.ValueWrite(&buffer)

	return buffer.String()
}

func (h *ViaHeader) ValueWrite(buffer io.StringWriter) {
	buffer.WriteString(h.ProtocolName)
	buffer.WriteString("/")
	buffer.WriteString(h.ProtocolVersion)
	buffer.WriteString("/")
	buffer.WriteString(h.Transport)
	buffer.WriteString(" ")
	buffer.WriteString(h.Host)

	if h.Port > 0 {
		buffer.WriteString(":")
		buffer.WriteString(strconv.Itoa(h.Port))
	}

	if h.Params != nil && h.Params.Length() > 0 {
		buffer.WriteString(";")
		h.Params.WriteTo(';', buffer)
	}
}

// Return an exact copy of this ViaHeader.
func (h *ViaHeader) Clone() *ViaHeader {
	newHop := &ViaHeader{
		ProtocolName:    h.ProtocolName,
		ProtocolVersion: h.ProtocolVersion,
		Transport:       h.Transport,
		Host:            h.Host,
	}
	if h.Port > 0 {
		newHop.Port = h.Port
	}
	if h.Params != nil {
		newHop.Params = h.Params.Clone()
	}

	return newHop
}

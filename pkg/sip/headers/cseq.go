package headers

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/sip/types"
)

// CSeq is CSeq header
type CSeqHeader struct {
	headerWriter

	SeqNo      uint32
	MethodName types.RequestMethod
}

// CSeqHeader
func (h *CSeqHeader) String() string {
	var buffer strings.Builder
	h.Write(&buffer)
	return buffer.String()
}

func (h *CSeqHeader) Name() string { return "CSeq" }

func (h *CSeqHeader) Value() string {
	return fmt.Sprintf("%d %s", h.SeqNo, h.MethodName)
}

func (h *CSeqHeader) ValueWrite(buffer io.StringWriter) {
	buffer.WriteString(strconv.Itoa(int(h.SeqNo)))
	buffer.WriteString(" ")
	buffer.WriteString(string(h.MethodName))
}

func (h *CSeqHeader) Clone() Header {
	if h == nil {
		return new(CSeqHeader)
	}

	return &CSeqHeader{
		SeqNo:      h.SeqNo,
		MethodName: h.MethodName,
	}
}

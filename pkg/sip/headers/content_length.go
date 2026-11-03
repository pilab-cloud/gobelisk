package headers

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ContentLengthHeader is Content-Length header representation
type ContentLengthHeader uint32

func (h ContentLengthHeader) String() string {
	var buffer strings.Builder
	h.Write(&buffer)
	return buffer.String()
}

func (h ContentLengthHeader) Write(w io.Writer) {
	fmt.Fprintf(w, "%s: %s", h.Name(), h.Value())
}

func (h *ContentLengthHeader) Name() string { return "Content-Length" }

func (h ContentLengthHeader) Value() string { return strconv.Itoa(int(h)) }

func (h *ContentLengthHeader) Clone() Header {
	var newCntLen ContentLengthHeader
	newCntLen = *h

	return &newCntLen
}

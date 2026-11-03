package sip

import (
	"strings"

	"github.com/devopshaven/gobelisk/pkg/sip/headers"
)

type Header = headers.Header

type Headers struct {
	// headers the sip message headers
	headers map[string]Header

	keys []string
}

// GetHeader returns Header if exists, otherwise nil is returned
// Use lower case to avoid allocs
// Headers are pointers, always Clone them for change
func (hs *Headers) GetHeader(name string) Header {
	name = strings.ToLower(name)

	return hs.headers[name]
}

// RemoveHeader removes header by name
func (hs *Headers) RemoveHeader(name string) {
	name = strings.ToLower(name)
	delete(hs.headers, name)
}

// CloneHeaders returns all cloned headers in slice.
func (hs *Headers) CloneHeaders() []Header {
	hdrs := make([]Header, len(hs.headers))

	for _, h := range hs.headers {
		hdrs = append(hdrs, h.Clone())
	}

	return hdrs
}

// Here are most used headers with quick reference

// CallID returns CallID parsed header or nil if not exists
// func (hs *Headers) CallID() *headers.CallIDHeader {
// 	if c, ok := hs.headers["call-id"]; ok {
// 		if h, ok := c.(*headers.CallIDHeader); ok {
// 			return h
// 		}
// 	}

// 	return nil
// }

// // Via returns Via parsed header or nil if not exists
// func (hs *Headers) Via() *headers.ViaHeader {
// 	if hs.via == nil {
// 		h := new(headers.ViaHeader)
// 		if parseHeaderLazy(hs, parseViaHeader, []string{"via", "v"}, h) {
// 			hs.via = h
// 		}
// 	}

// 	return hs.via
// }

// // From returns From parsed header or nil if not exists
// func (hs *Headers) From() *headers.FromHeader {
// 	if hs.from == nil {
// 		h := &FromHeader{}
// 		if parseHeaderLazy(hs, parseFromHeader, []string{"from", "f"}, h) {
// 			hs.from = h
// 		}
// 	}

// 	return hs.from
// }

// // To returns To parsed header or nil if not exists
// func (hs *Headers) To() *headers.ToHeader {
// 	if hs.to == nil {
// 		h := new(headers.ToHeader)
// 		if parseHeaderLazy(hs, parseToHeader, []string{"to", "t"}, h) {
// 			hs.to = h
// 		}
// 	}

// 	return hs.to
// }

// // CSeq returns CSEQ parsed header or nil if not exists
// func (hs *Headers) CSeq() *headers.CSeqHeader {
// 	if hs.cseq == nil {
// 		h := &CSeqHeader{}
// 		if parseHeaderLazy(hs, parseCSeqHeader, []string{"cseq"}, h) {
// 			hs.cseq = h
// 		}
// 	}
// 	return hs.cseq
// }

// // MaxForwards returns Max-Forwards parsed header or nil if not exists
// func (hs *Headers) MaxForwards() *headers.MaxForwardsHeader {
// 	if hs.maxForwards == nil {
// 		var h MaxForwardsHeader
// 		if parseHeaderLazy(hs, parseMaxForwardsHeader, []string{"max-forwards"}, &h) {
// 			hs.maxForwards = &h
// 		}
// 	}
// 	return hs.maxForwards
// }

// // ContentLength returns Content-Length parsed header or nil if not exists
// func (hs *Headers) ContentLength() *headers.ContentLengthHeader {
// 	if hs.contentLength == nil {
// 		var h ContentLengthHeader
// 		if parseHeaderLazy(hs, parseContentLengthHeader, []string{"content-length", "l"}, &h) {
// 			hs.contentLength = &h
// 		}
// 	}

// 	return hs.contentLength
// }

// // ContentType returns Content-Type parsed header or nil if not exists
// func (hs *Headers) ContentType() *headers.ContentTypeHeader {
// 	if hs.contentType == nil {
// 		var h ContentTypeHeader
// 		if parseHeaderLazy(hs, parseContentTypeHeader, []string{"content-type", "c"}, &h) {
// 			hs.contentType = &h
// 		}
// 	}

// 	return hs.contentType
// }

// // Contact returns Contact parsed header or nil if not exists
// func (hs *Headers) Contact() *headers.ContactHeader {
// 	if hs.contact == nil {
// 		h := &ContactHeader{}
// 		if parseHeaderLazy(hs, parseContactHeader, []string{"contact", "m"}, h) {
// 			hs.contact = h
// 		}
// 	}

// 	return hs.contact
// }

// // Route returns Route parsed header or nil if not exists
// func (hs *Headers) Route() *headers.RouteHeader {
// 	if hs.route == nil {
// 		h := &RouteHeader{}
// 		if parseHeaderLazy(hs, parseRouteHeader, []string{"route"}, h) {
// 			hs.route = h
// 		}
// 	}
// 	return hs.route
// }

// // RecordRoute returns Record-Route parsed header or nil if not exists
// func (hs *Headers) RecordRoute() *headers.RecordRouteHeader {
// 	if hs.recordRoute == nil {
// 		h := &RecordRouteHeader{}
// 		if parseHeaderLazy(hs, parseRecordRouteHeader, []string{"record-route"}, h) {
// 			hs.recordRoute = h
// 		}
// 	}
// 	return hs.recordRoute
// }

// ! TODO: implement

func CopyHeaders(from, to Message, headerNames ...string) {
	for _, name := range headerNames {
		CopyHeader(from, to, name)
	}
}

// CopyHeader copies the header from one message to another.
// returns true if header exists, false otherwise
func CopyHeader(from, to Message, name string) bool {
	h := from.GetHeader(name)
	if h != nil {
		to.AppendHeader(h.Clone())

		return true
	}

	return false
}

package sip

import (
	"io"

	"github.com/devopshaven/gobelisk/pkg/sip/headers"
	"github.com/rs/zerolog/log"
)

type MessageHandler func(msg Message)

// StatusCode - response status code: 1xx - 6xx
type StatusCode int

const (
	// https://datatracker.ietf.org/doc/html/rfc3261#section-21
	StatusTrying            StatusCode = 100
	StatusRinging           StatusCode = 180
	StatusCallIsForwarded   StatusCode = 181
	StatusQueued            StatusCode = 182
	StatusSessionInProgress StatusCode = 183

	StatusOK StatusCode = 200

	StatusMovedPermanently StatusCode = 301
	StatusMovedTemporarily StatusCode = 302
	StatusUseProxy         StatusCode = 305

	StatusBadRequest                   StatusCode = 400
	StatusUnauthorized                 StatusCode = 401
	StatusPaymentRequired              StatusCode = 402
	StatusForbidden                    StatusCode = 403
	StatusNotFound                     StatusCode = 404
	StatusMethodNotAllowed             StatusCode = 405
	StatusNotAcceptable                StatusCode = 406
	StatusProxyAuthRequired            StatusCode = 407
	StatusRequestTimeout               StatusCode = 408
	StatusConflict                     StatusCode = 409
	StatusGone                         StatusCode = 410
	StatusRequestEntityTooLarge        StatusCode = 413
	StatusRequestURITooLong            StatusCode = 414
	StatusUnsupportedMediaType         StatusCode = 415
	StatusRequestedRangeNotSatisfiable StatusCode = 416
	StatusBadExtension                 StatusCode = 420
	StatusExtensionRequired            StatusCode = 421
	StatusIntervalToBrief              StatusCode = 423
	StatusTemporarilyUnavailable       StatusCode = 480
	StatusCallTransactionDoesNotExists StatusCode = 481
	StatusLoopDetected                 StatusCode = 482
	StatusTooManyHops                  StatusCode = 483
	StatusAddressIncomplete            StatusCode = 484
	StatusAmbiguous                    StatusCode = 485
	StatusBusyHere                     StatusCode = 486
	StatusRequestTerminated            StatusCode = 487
	StatusNotAcceptableHere            StatusCode = 488

	StatusInternalServerError StatusCode = 500
	StatusNotImplemented      StatusCode = 501
	StatusBadGateway          StatusCode = 502
	StatusServiceUnavailable  StatusCode = 503
	StatusGatewayTimeout      StatusCode = 504
	StatusVersionNotSupported StatusCode = 505
	StatusMessageTooLarge     StatusCode = 513

	StatusGlobalBusyEverywhere       StatusCode = 600
	StatusGlobalDecline              StatusCode = 603
	StatusGlobalDoesNotExistAnywhere StatusCode = 604
	StatusGlobalNotAcceptable        StatusCode = 606
)

type Message interface {
	// String returns string representation of SIP message in RFC 3261 form.
	String() string

	// String write is same as String but lets you to provide writter and reduce allocations
	StringWrite(io.StringWriter)

	// GetHeaders returns slice of headers of the given type.
	GetHeader(name string) Header

	// AppendHeader appends header to message.
	AppendHeader(header Header)

	// CallID returns 'Call-ID' header.
	CallID() *headers.CallIDHeader

	// Via returns the top 'Via' header field.
	Via() *headers.ViaHeader

	// From returns 'From' header field.
	From() *headers.FromHeader

	// To returns 'To' header field.
	To() *headers.ToHeader

	// CSeq returns 'CSeq' header field.
	CSeq() *headers.CSeqHeader

	// Body returns message body.
	Body() []byte

	// SetBody sets message body.
	SetBody(body []byte)

	Transport() string
	SetTransport(tp string)
	Source() string
	SetSource(src string)
	Destination() string
	SetDestination(dest string)
}

type MessageData struct {
	// message Headers
	Headers

	// Set to 2.0 version by default
	SipVersion string
	body       []byte

	transport string

	// This is for internal routing
	src  string
	dest string
}

func (msg *MessageData) Body() []byte {
	return msg.body
}

// SetBody sets message body, calculates it length and add 'Content-Length' header.
func (msg *MessageData) SetBody(body []byte) {
	msg.body = body

	log.Error().Msgf("The content-length header is removed in sip.Message.SetBody()")
}

func (msg *MessageData) Transport() string {
	return msg.transport
}

func (msg *MessageData) SetTransport(tp string) {
	msg.transport = tp
}

func (msg *MessageData) Source() string {
	return msg.src
}

func (msg *MessageData) SetSource(src string) {
	msg.src = src
}

func (msg *MessageData) Destination() string {
	return msg.dest
}

func (msg *MessageData) SetDestination(dest string) {
	msg.dest = dest
}

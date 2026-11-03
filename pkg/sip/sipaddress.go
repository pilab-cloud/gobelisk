package sip

import (
	"bytes"
	"io"
	"strconv"
	"strings"
)

// Address is parsed form of
// sip:user:password@host:port;uri-parameters?headers
// In case of `sips:“ Encrypted is set to true
type Address struct {
	// True if and only if the URI is a SIPS addr.
	Encrypted bool
	Wildcard  bool

	// The user part of the URI: the 'joe' in sip:joe@bloggs.com
	// This is a pointer, so that URIs without a user part can have 'nil'.
	User string

	// The password field of the addr. This is represented in the URI as joe:hunter2@bloggs.com.
	// Note that if a URI has a password field, it *must* have a user field as well.
	// Note that RFC 3261 strongly recommends against the use of password fields in SIP URIs,
	// as they are fundamentally insecure.
	Password string

	// The host part of the addr. This can be a domain, or a string representation of an IP address.
	Host string

	// The port part of the addr. This is optional, and can be empty.
	Port int

	// Any parameters associated with the addr.
	// These are used to provide information about requests that may be constructed from the addr.
	// (For more details, see RFC 3261 section 19.1.1).
	// These appear as a semicolon-separated list of key=value pairs following the host[:port] part.
	UriParams Params

	// Any headers to be included on requests constructed from this addr.
	// These appear as a '&'-separated list at the end of the URI, introduced by '?'.
	Headers Params
}

// Generates the string representation of a SipUri struct.
func (addr *Address) String() string {
	var buffer strings.Builder
	addr.StringWrite(&buffer)

	return buffer.String()
}

func (addr *Address) WriteTo(w io.Writer) (int64, error) {
	// TODO: rewrite it to use StringWrite
	buf := bytes.NewBufferString("")
	addr.StringWrite(buf)

	n, err := w.Write(buf.Bytes())
	return int64(n), err
}

// StringWrite writes uri string to buffer
func (addr *Address) StringWrite(buffer io.StringWriter) {
	// Compulsory protocol identifier.
	if addr.IsEncrypted() {
		buffer.WriteString("sips")
		buffer.WriteString(":")
	} else {
		buffer.WriteString("sip")
		buffer.WriteString(":")
	}

	// Optional userinfo part.
	if addr.User != "" {
		buffer.WriteString(addr.User)
		if addr.Password != "" {
			buffer.WriteString(":")
			buffer.WriteString(addr.Password)
		}
		buffer.WriteString("@")
	}

	// Compulsory hostname.
	buffer.WriteString(addr.Host)

	// Optional port number.
	if addr.Port > 0 {
		buffer.WriteString(":")
		buffer.WriteString(strconv.Itoa(addr.Port))
	}

	if (addr.UriParams != nil) && addr.UriParams.Length() > 0 {
		buffer.WriteString(";")
		buffer.WriteString(addr.UriParams.ToString(';'))
	}

	if (addr.Headers != nil) && addr.Headers.Length() > 0 {
		buffer.WriteString("?")
		buffer.WriteString(addr.Headers.ToString('&'))
	}
}

// Clone returns copy of address
func (addr *Address) Clone() *Address {
	c := *addr

	return &c
}

// IsEncrypted returns true if uri is SIPS uri
func (addr *Address) IsEncrypted() bool {
	return addr.Encrypted
}

// Addr is uri address form. sip:user@host:port
func (addr *Address) Addr() string {
	a := addr.User + "@" + addr.Host
	if addr.Port > 0 {
		a += ":" + strconv.Itoa(addr.Port)
	}

	if addr.Encrypted {
		return "sips:" + a
	}

	return "sip:" + a
}

// HostPort represents host:port part
func (addr *Address) HostPort() string {
	p := strconv.Itoa(addr.Port)
	return addr.Host + ":" + p
}

type SipAddressBuilder struct {
	sipAddress *Address
}

// SipAddressBuilder is a builder for SipAddress
//
//	address := NewAddressBuilder().
//	  WithEncryption(false).
//	  WithUsername("alice").
//	  WithPassword("secret").
//	  WithDomain("example.com").
//	  WithPort("5060").
//	  WithHeader("Call-ID", "d51a3d39e358").
//	  WithParam("tag", "d51a3d39e358").
//	  Build()
func NewAddressBuilder() *SipAddressBuilder {
	return &SipAddressBuilder{}
}

func (builder *SipAddressBuilder) WithEncryption(encrypted bool) *SipAddressBuilder {
	builder.sipAddress.Encrypted = encrypted
	return builder
}

func (builder *SipAddressBuilder) WithUsername(username string) *SipAddressBuilder {
	builder.sipAddress.User = username
	return builder
}

func (builder *SipAddressBuilder) WithDomain(domain string) *SipAddressBuilder {
	builder.sipAddress.Host = domain
	return builder
}

func (builder *SipAddressBuilder) WithPort(port string) *SipAddressBuilder {
	p, _ := strconv.Atoi(port)
	builder.sipAddress.Port = p
	return builder
}

func (builder *SipAddressBuilder) WithParam(param, value string) *SipAddressBuilder {
	builder.sipAddress.UriParams.Set(param, value)

	return builder
}

func (builder *SipAddressBuilder) WithHeader(header, value string) *SipAddressBuilder {
	builder.sipAddress.Headers.Set(header, value)

	return builder
}

func (builder *SipAddressBuilder) WithPassword(password string) *SipAddressBuilder {
	builder.sipAddress.Password = password
	return builder
}

func (builder *SipAddressBuilder) Build() *Address {
	return builder.sipAddress
}

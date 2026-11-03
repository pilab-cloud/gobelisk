package types

// Address represents SIP address
type Address interface {
	// String returns string representation of address
	String() string

	// Addr returns only sip:user@host:port. Useful to create <sip:user@host:port> addresses
	Addr() string

	// Clone returns copy of address
	Clone() Address
}

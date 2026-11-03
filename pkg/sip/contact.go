package sip

type ContactRef struct {
	// The display name from the header, may be omitted.
	DisplayName string
	Address     Address
	// Any parameters present in the header.
	Params Params
}

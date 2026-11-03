package auth

import (
	"errors"
)

var (
	ErrAuthParseFailed  = errors.New("failed to parse credentials")
	ErrUserNotFound     = errors.New("user not found")
	ErrDigestCalcFailed = errors.New("digest calculation failed")
	ErrDigestMismatch   = errors.New("digest mismatch")
)

type Authenticator interface {
	AuthHeader() string
	Authenticate(authHeader string) error
}

// NoneAuthenticator accepts all requests without authentication.
type NoneAuthenticator struct{}

func (NoneAuthenticator) AuthHeader() string { return "" }

func (NoneAuthenticator) Authenticate(string) error { return nil }

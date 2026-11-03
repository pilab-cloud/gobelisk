package auth

import (
	"github.com/icholy/digest"
	"github.com/rs/xid"
	"github.com/rs/zerolog/log"
)

type DefaultAuthenticator struct {
	realm      string
	challenges map[string]*digest.Challenge
	extensions map[string]string
}

func BuildDefaultAuthenticator() *DefaultAuthenticator {
	return &DefaultAuthenticator{
		realm:      "go-belisk",
		challenges: make(map[string]*digest.Challenge),
		extensions: map[string]string{
			"100": "default",
			"200": "default",
		},
	}
}

func (a *DefaultAuthenticator) AuthHeader() string {
	return a.newChallenge().String()
}

func (a *DefaultAuthenticator) newChallenge() *digest.Challenge {
	ch := &digest.Challenge{
		Realm:     a.realm,
		Nonce:     xid.New().String(),
		Algorithm: "MD5",
		Opaque:    "opaque",
		// QOP:       []string{"auth"},
	}

	log.Trace().Msgf("created new challenge: %s", ch.String())
	a.challenges[ch.Nonce] = ch

	return ch
}

// FindChallenge finds a challenge by nonce
func (a *DefaultAuthenticator) FindChallenge(nonce string) (*digest.Challenge, error) {
	challenge, exists := a.challenges[nonce]
	if !exists {
		return nil, digest.ErrNoChallenge
	}

	return challenge, nil
}

// Authorize a SIP request
func (a *DefaultAuthenticator) Authenticate(authHeader string) error {
	cred, err := digest.ParseCredentials(authHeader)
	if err != nil {
		return ErrAuthParseFailed
	}

	// Check registry
	passwd, exists := a.extensions[cred.Username]
	if !exists {
		return ErrUserNotFound
	}

	// TODO: find a better way to reconcile this
	chal, err := a.FindChallenge(cred.Nonce)
	if err != nil {
		return err
	}

	digCred, err := digest.Digest(chal, digest.Options{
		Method:   "REGISTER",
		URI:      cred.URI,
		Username: cred.Username,
		Password: passwd,
	})

	if err != nil {
		return ErrDigestCalcFailed
	}

	if cred.Response != digCred.Response {
		return ErrDigestMismatch
	}

	return nil
}

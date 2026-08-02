// Package xoauth2 speaks Google's XOAUTH2 SASL mechanism for both SMTP and
// IMAP.
//
// The mechanism is one line: a single initial response carrying the address
// and a bearer token. It exists because the mail protocols predate OAuth by
// decades and can only carry "an authentication string" — so the token rides
// where a password would have.
package xoauth2

import (
	"errors"
	"fmt"
	"net/smtp"

	"github.com/emersion/go-sasl"
)

func initialResponse(user, token string) []byte {
	return fmt.Appendf(nil, "user=%s\x01auth=Bearer %s\x01\x01", user, token)
}

// ---------------------------------------------------------------- IMAP side

type saslClient struct {
	user, token string
	failed      bool
}

// NewSASL returns a client for go-imap's Authenticate.
func NewSASL(user, token string) sasl.Client {
	return &saslClient{user: user, token: token}
}

func (c *saslClient) Start() (string, []byte, error) {
	return "XOAUTH2", initialResponse(c.user, c.token), nil
}

// Next handles the error path: on failure the server sends a base64 JSON
// blob and expects an empty line before it issues the final NO. Answering
// anything else hangs the exchange.
func (c *saslClient) Next(challenge []byte) ([]byte, error) {
	if c.failed {
		return nil, errors.New("xoauth2: authentication rejected")
	}
	c.failed = true
	return []byte{}, nil
}

// ---------------------------------------------------------------- SMTP side

type smtpAuth struct {
	user, token string
}

// NewSMTP returns an implementation of net/smtp's Auth.
func NewSMTP(user, token string) smtp.Auth {
	return &smtpAuth{user: user, token: token}
}

func (a *smtpAuth) Start(_ *smtp.ServerInfo) (string, []byte, error) {
	return "XOAUTH2", initialResponse(a.user, a.token), nil
}

func (a *smtpAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		// Same dance as IMAP: an empty response lets the server finish
		// refusing us with a readable error instead of a stalled connection.
		return []byte{}, nil
	}
	return nil, nil
}

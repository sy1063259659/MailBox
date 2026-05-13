package imapmail

import "github.com/emersion/go-sasl"

type xoauth2Client struct {
	email       string
	accessToken string
	sent        bool
}

var _ sasl.Client = (*xoauth2Client)(nil)

func newXOAUTH2Client(email, accessToken string) sasl.Client {
	return &xoauth2Client{email: email, accessToken: accessToken}
}

func (c *xoauth2Client) Start() (string, []byte, error) {
	c.sent = true
	return "XOAUTH2", []byte("user=" + c.email + "\x01auth=Bearer " + c.accessToken + "\x01\x01"), nil
}

func (c *xoauth2Client) Next(_ []byte) ([]byte, error) {
	if c.sent {
		return nil, sasl.ErrUnexpectedServerChallenge
	}
	c.sent = true
	return []byte("user=" + c.email + "\x01auth=Bearer " + c.accessToken + "\x01\x01"), nil
}

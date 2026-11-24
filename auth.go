package picnic

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// Login signs in with the email and password of a Picnic account. When
// Picnic wants a second factor it returns ErrSecondFactorRequired; the client
// then holds a provisional token, and RequestCode and VerifyCode complete
// the sign-in.
func (c *Client) Login(ctx context.Context, email, password string) error {
	sum := md5.Sum([]byte(password))
	in := map[string]any{"key": email, "secret": hex.EncodeToString(sum[:]), "client_id": clientID}
	const path = "/user/login"
	status, body, err := c.call(ctx, http.MethodPost, path, in)
	if err != nil {
		return err
	}
	var out struct {
		SecondFactor bool `json:"second_factor_authentication_required"`
	}
	_ = json.Unmarshal(body, &out) // decode reports a body that doesn't parse
	if out.SecondFactor {
		return ErrSecondFactorRequired
	}
	if err := decode(path, status, body, nil); err != nil {
		return err
	}
	if c.Token() == "" {
		return &Error{Endpoint: path, Status: status, Message: "no auth token in the response"}
	}
	return nil
}

// Channel is where Picnic sends a one-time code.
type Channel string

const (
	SMS   Channel = "SMS"
	Email Channel = "EMAIL"
)

// RequestCode asks Picnic to send a one-time code after Login returned
// ErrSecondFactorRequired.
func (c *Client) RequestCode(ctx context.Context, ch Channel) error {
	const path = "/user/2fa/generate"
	status, body, err := c.call(ctx, http.MethodPost, path, map[string]string{"channel": string(ch)})
	if err != nil {
		return err
	}
	return decode(path, status, body, nil)
}

// VerifyCode completes a sign-in with the one-time code the user received.
// A wrong code is an *Error with Picnic's error code.
func (c *Client) VerifyCode(ctx context.Context, code string) error {
	const path = "/user/2fa/verify"
	status, body, err := c.call(ctx, http.MethodPost, path, map[string]string{"otp": code})
	if err != nil {
		return err
	}
	return decode(path, status, body, nil)
}

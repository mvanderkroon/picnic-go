// Command picnic-login signs in to Picnic and prints the auth token, for use
// with picnic.WithToken and the live tests. It reads the account from
// PICNIC_EMAIL and PICNIC_PASSWORD and, when Picnic asks for one, the SMS
// code from standard input.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mvanderkroon/picnic-go"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	email, password := os.Getenv("PICNIC_EMAIL"), os.Getenv("PICNIC_PASSWORD")
	if email == "" || password == "" {
		return errors.New("set PICNIC_EMAIL and PICNIC_PASSWORD")
	}
	ctx := context.Background()
	c := picnic.New()
	err := c.Login(ctx, email, password)
	if errors.Is(err, picnic.ErrSecondFactorRequired) {
		if err := c.RequestCode(ctx, picnic.SMS); err != nil {
			return err
		}
		fmt.Fprint(os.Stderr, "SMS code: ")
		code, rerr := bufio.NewReader(os.Stdin).ReadString('\n')
		if rerr != nil {
			return rerr
		}
		err = c.VerifyCode(ctx, strings.TrimSpace(code))
	}
	if err != nil {
		return err
	}
	fmt.Println(c.Token())
	return nil
}

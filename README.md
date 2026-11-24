# picnic-go

An unofficial Go client for the API behind the [Picnic](https://picnic.app)
grocery delivery app: sign in (including the SMS second factor), search for
products, read the basket and add to it.

```go
c := picnic.New() // NL; picnic.WithCountry("DE") for another storefront

err := c.Login(ctx, email, password)
if errors.Is(err, picnic.ErrSecondFactorRequired) {
	_ = c.RequestCode(ctx, picnic.SMS)
	err = c.VerifyCode(ctx, codeFromUser)
}
if err != nil { ... }
saveSomewhere(c.Token()) // later: picnic.New(picnic.WithToken(token))

products, _ := c.Search(ctx, "uien")
cart, _ := c.AddProduct(ctx, products[0].ID, 2)
```

Picnic may replace the auth token on any response, so read `Token()` after
calls if you persist it. When Picnic rejects the token, errors satisfy
`errors.Is(err, picnic.ErrUnauthorized)` and you have to sign in again. Other
failures are a `*picnic.Error` with the endpoint, the HTTP status and Picnic's
error code.

## Status and caveats

Picnic does not publish this API. It can change whenever Picnic updates its
app, and using it may be against Picnic's terms of service. Use it for your
own account, at your own risk. The package covers only the calls above; it
does not place orders, pick delivery slots or read deliveries.

## Getting a token and the live test

```sh
PICNIC_EMAIL=you@example.com PICNIC_PASSWORD="$(pass show picnic | head -1)" go run ./cmd/picnic-login
```

prints a token, asking for the SMS code if Picnic wants one. Fill in the
password from your password manager with `$(…)` as above, so that it stays
out of your shell history. The regular tests
run against a fake server; `TestLive` calls the real API with that token (it
searches and reads the basket, never changes it):

```sh
PICNIC_TOKEN=... go test -run Live -v
```

## Credits

The endpoints, headers and login flow follow
[python-picnic-api2](https://github.com/codesalatdev/python-picnic-api)
(Apache-2.0), which is the first place to look when something stops working.

## Licence

Apache-2.0, see [LICENSE](LICENSE).

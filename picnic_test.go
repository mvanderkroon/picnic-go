package picnic_test

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/mvanderkroon/picnic-go"
)

// fakePicnic stands in for Picnic's API. Each test registers what it
// expects to be called, as "METHOD /path"; anything else fails the test.
type fakePicnic struct {
	t        *testing.T
	handlers map[string]http.HandlerFunc
}

func newFake(t *testing.T) (*fakePicnic, *httptest.Server) {
	f := &fakePicnic{t: t, handlers: map[string]http.HandlerFunc{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-picnic-agent"); got == "" {
			t.Errorf("%s %s: no x-picnic-agent header", r.Method, r.URL.Path)
		}
		h, ok := f.handlers[r.Method+" "+r.URL.Path]
		if !ok {
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakePicnic) on(route string, h http.HandlerFunc) { f.handlers[route] = h }

func reply(w http.ResponseWriter, status int, token, body string) {
	if token != "" {
		w.Header().Set("x-picnic-auth", token)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	io.WriteString(w, body)
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readJSON(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		t.Fatalf("%s: body is not JSON: %v", r.URL.Path, err)
	}
	return m
}

func requireToken(t *testing.T, r *http.Request, want string) {
	t.Helper()
	if got := r.Header.Get("x-picnic-auth"); got != want {
		t.Errorf("%s: auth token %q, want %q", r.URL.Path, got, want)
	}
}

var ctx = context.Background()

func TestLoginWithoutSecondFactorSignsIn(t *testing.T) {
	f, srv := newFake(t)
	f.on("POST /user/login", func(w http.ResponseWriter, r *http.Request) {
		body := readJSON(t, r)
		sum := md5.Sum([]byte("hunter2"))
		want := map[string]any{"key": "a@example.com", "secret": hex.EncodeToString(sum[:]), "client_id": float64(30100)}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("login body %v, want %v", body, want)
		}
		reply(w, 200, "tok-1", `{"user_id":"u1","second_factor_authentication_required":false}`)
	})
	f.on("GET /pages/search-page-results", func(w http.ResponseWriter, r *http.Request) {
		requireToken(t, r, "tok-1")
		reply(w, 200, "", `{"body":{"child":{}}}`)
	})

	c := picnic.New(picnic.WithBaseURL(srv.URL))
	if err := c.Login(ctx, "a@example.com", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if got := c.Token(); got != "tok-1" {
		t.Errorf("Token() = %q, want tok-1", got)
	}
	if _, err := c.Search(ctx, "uien"); err != nil {
		t.Fatal(err)
	}
}

func TestLoginWithSecondFactorCompletesWithTheCode(t *testing.T) {
	f, srv := newFake(t)
	f.on("POST /user/login", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, "provisional", `{"second_factor_authentication_required":true}`)
	})
	f.on("POST /user/2fa/generate", func(w http.ResponseWriter, r *http.Request) {
		requireToken(t, r, "provisional")
		if got := readJSON(t, r)["channel"]; got != "SMS" {
			t.Errorf("channel %v, want SMS", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	f.on("POST /user/2fa/verify", func(w http.ResponseWriter, r *http.Request) {
		requireToken(t, r, "provisional")
		if got := readJSON(t, r)["otp"]; got != "123456" {
			t.Errorf("otp %v, want 123456", got)
		}
		reply(w, 200, "tok-2", "")
	})

	c := picnic.New(picnic.WithBaseURL(srv.URL))
	err := c.Login(ctx, "a@example.com", "hunter2")
	if !errors.Is(err, picnic.ErrSecondFactorRequired) {
		t.Fatalf("Login error %v, want ErrSecondFactorRequired", err)
	}
	if err := c.RequestCode(ctx, picnic.SMS); err != nil {
		t.Fatal(err)
	}
	if err := c.VerifyCode(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	if got := c.Token(); got != "tok-2" {
		t.Errorf("Token() = %q, want tok-2", got)
	}
}

func TestLoginWithWrongPasswordIsUnauthorized(t *testing.T) {
	f, srv := newFake(t)
	f.on("POST /user/login", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 401, "", `{"error":{"code":"AUTH_INVALID_CRED","message":"Invalid credentials"}}`)
	})
	err := picnic.New(picnic.WithBaseURL(srv.URL)).Login(ctx, "a@example.com", "wrong")
	if !errors.Is(err, picnic.ErrUnauthorized) {
		t.Fatalf("error %v, want ErrUnauthorized", err)
	}
}

func TestLoginWithoutATokenInTheResponseFails(t *testing.T) {
	f, srv := newFake(t)
	f.on("POST /user/login", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, "", `{"user_id":"u1"}`)
	})
	if err := picnic.New(picnic.WithBaseURL(srv.URL)).Login(ctx, "a@example.com", "hunter2"); err == nil {
		t.Fatal("Login succeeded without a token")
	}
}

func TestWrongCodeReportsPicnicsErrorCode(t *testing.T) {
	f, srv := newFake(t)
	f.on("POST /user/2fa/verify", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 400, "", `{"error":{"code":"OTP_INVALID","message":"The code is not valid"}}`)
	})
	err := picnic.New(picnic.WithBaseURL(srv.URL), picnic.WithToken("provisional")).VerifyCode(ctx, "000000")
	var pe *picnic.Error
	if !errors.As(err, &pe) || pe.Code != "OTP_INVALID" {
		t.Fatalf("error %v, want an *Error with code OTP_INVALID", err)
	}
	if errors.Is(err, picnic.ErrUnauthorized) {
		t.Error("a wrong code is reported as ErrUnauthorized")
	}
}

func TestSearchReturnsTheResultTilesInPageOrder(t *testing.T) {
	f, srv := newFake(t)
	f.on("GET /pages/search-page-results", func(w http.ResponseWriter, r *http.Request) {
		requireToken(t, r, "tok")
		if got := r.URL.Query().Get("search_term"); got != "rode uien & co" {
			t.Errorf("search_term %q", got)
		}
		reply(w, 200, "", fixture(t, "search.json"))
	})
	got, err := picnic.New(picnic.WithBaseURL(srv.URL), picnic.WithToken("tok")).Search(ctx, "rode uien & co")
	if err != nil {
		t.Fatal(err)
	}
	want := []picnic.Product{
		{ID: "s1001234", Name: "Picnic uien", Price: 129, UnitQuantity: "1 kilo", UnitQuantitySub: "€1.29/kg", MaxCount: 99, ImageID: "img-uien"},
		{ID: "s1005678", Name: "Rode uien", Price: 199, UnitQuantity: "500 gram", MaxCount: 50, ImageID: "img-rode-uien"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Search =\n%+v\nwant\n%+v", got, want)
	}
}

func TestSearchWithoutResultsIsEmpty(t *testing.T) {
	f, srv := newFake(t)
	f.on("GET /pages/search-page-results", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, "", `{"body":{"child":{"type":"TEXT","markdown":"Geen resultaten"}}}`)
	})
	got, err := picnic.New(picnic.WithBaseURL(srv.URL), picnic.WithToken("tok")).Search(ctx, "xyzzy")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Search = %v, %v; want an empty slice", got, err)
	}
}

func TestUnreadableResponsesNameTheEndpoint(t *testing.T) {
	for name, body := range map[string]string{
		"no page body": `{"something":"else"}`,
		"not JSON":     `<html>maintenance</html>`,
	} {
		t.Run(name, func(t *testing.T) {
			f, srv := newFake(t)
			f.on("GET /pages/search-page-results", func(w http.ResponseWriter, r *http.Request) {
				reply(w, 200, "", body)
			})
			_, err := picnic.New(picnic.WithBaseURL(srv.URL), picnic.WithToken("tok")).Search(ctx, "uien")
			var pe *picnic.Error
			if !errors.As(err, &pe) || pe.Endpoint != "/pages/search-page-results" {
				t.Fatalf("error %v, want an *Error for /pages/search-page-results", err)
			}
		})
	}
}

func TestRejectedTokenIsUnauthorized(t *testing.T) {
	f, srv := newFake(t)
	// Picnic reports an expired token with status 200 and an error body.
	f.on("GET /cart", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, "", `{"error":{"code":"AUTH_ERROR","message":"Token expired"}}`)
	})
	_, err := picnic.New(picnic.WithBaseURL(srv.URL), picnic.WithToken("old")).Cart(ctx)
	if !errors.Is(err, picnic.ErrUnauthorized) {
		t.Fatalf("error %v, want ErrUnauthorized", err)
	}
}

func TestAddProductReturnsTheBasket(t *testing.T) {
	f, srv := newFake(t)
	f.on("POST /cart/add_product", func(w http.ResponseWriter, r *http.Request) {
		requireToken(t, r, "tok")
		body := readJSON(t, r)
		want := map[string]any{"product_id": "s1001234", "count": float64(2)}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("add_product body %v, want %v", body, want)
		}
		reply(w, 200, "", fixture(t, "cart.json"))
	})
	got, err := picnic.New(picnic.WithBaseURL(srv.URL), picnic.WithToken("tok")).AddProduct(ctx, "s1001234", 2)
	if err != nil {
		t.Fatal(err)
	}
	want := &picnic.Cart{
		Lines: []picnic.CartLine{
			{ProductID: "s1001234", Name: "Picnic uien", UnitQuantity: "1 kilo", Count: 2, Price: 258},
			{ProductID: "s1005678", Name: "Rode uien", UnitQuantity: "500 gram", Count: 1, Price: 199},
		},
		TotalCount: 3,
		TotalPrice: 457,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AddProduct =\n%+v\nwant\n%+v", got, want)
	}
}

func TestServerErrorIsAnErrorWithItsStatus(t *testing.T) {
	f, srv := newFake(t)
	f.on("POST /cart/add_product", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 503, "", `{"message":"Service unavailable"}`)
	})
	_, err := picnic.New(picnic.WithBaseURL(srv.URL), picnic.WithToken("tok")).AddProduct(ctx, "s1001234", 1)
	var pe *picnic.Error
	if !errors.As(err, &pe) || pe.Status != 503 || pe.Endpoint != "/cart/add_product" {
		t.Fatalf("error %v, want an *Error with status 503 for /cart/add_product", err)
	}
}

func TestCartReadsTheBasket(t *testing.T) {
	f, srv := newFake(t)
	f.on("GET /cart", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, "", fixture(t, "cart.json"))
	})
	got, err := picnic.New(picnic.WithBaseURL(srv.URL), picnic.WithToken("tok")).Cart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalCount != 3 || len(got.Lines) != 2 || got.Lines[0].Count != 2 {
		t.Errorf("Cart = %+v", got)
	}
}

func TestTokenPicnicReplacesIsUsedFromThenOn(t *testing.T) {
	f, srv := newFake(t)
	f.on("GET /cart", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-picnic-auth") == "old" {
			reply(w, 200, "new", fixture(t, "cart.json"))
			return
		}
		requireToken(t, r, "new")
		reply(w, 200, "", fixture(t, "cart.json"))
	})
	c := picnic.New(picnic.WithBaseURL(srv.URL), picnic.WithToken("old"))
	for range 2 {
		if _, err := c.Cart(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.Token(); got != "new" {
		t.Errorf("Token() = %q, want new", got)
	}
}

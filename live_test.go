package picnic_test

import (
	"os"
	"testing"

	"github.com/mvanderkroon/picnic-go"
)

// TestLive calls the real Picnic API with a token from cmd/picnic-login:
//
//	PICNIC_TOKEN=$(go run ./cmd/picnic-login) go test -run Live -v
//
// It searches and reads the basket; it never changes the basket.
func TestLive(t *testing.T) {
	token := os.Getenv("PICNIC_TOKEN")
	if token == "" {
		t.Skip("PICNIC_TOKEN not set")
	}
	c := picnic.New(picnic.WithToken(token))

	products, err := c.Search(ctx, "uien")
	if err != nil {
		t.Fatal(err)
	}
	if len(products) == 0 {
		t.Fatal("no products for \"uien\"; the search page layout may have changed")
	}
	for _, p := range products {
		if p.ID == "" || p.Name == "" || p.UnitQuantity == "" {
			t.Errorf("incomplete product %+v", p)
		}
	}
	t.Logf("first result: %+v", products[0])

	cart, err := c.Cart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("basket: %d products, %d cents", cart.TotalCount, cart.TotalPrice)
}

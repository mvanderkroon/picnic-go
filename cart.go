package picnic

import (
	"context"
	"net/http"
)

// Cart is the basket.
type Cart struct {
	Lines      []CartLine
	TotalCount int // the number of products, counting each unit
	TotalPrice int // in euro cents
}

// CartLine is one product in the basket and how many of it.
type CartLine struct {
	ProductID    string
	Name         string
	UnitQuantity string
	Count        int
	Price        int // the line's price in euro cents
}

type rawCart struct {
	Items []struct {
		Price        cents `json:"price"`
		DisplayPrice cents `json:"display_price"`
		Items        []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			UnitQuantity string `json:"unit_quantity"`
			Decorators   []struct {
				Type     string `json:"type"`
				Quantity int    `json:"quantity"`
			} `json:"decorators"`
		} `json:"items"`
	} `json:"items"`
	TotalCount int   `json:"total_count"`
	TotalPrice cents `json:"total_price"`
}

func (r rawCart) cart() *Cart {
	c := &Cart{Lines: []CartLine{}, TotalCount: r.TotalCount, TotalPrice: int(r.TotalPrice)}
	for _, line := range r.Items {
		price := line.DisplayPrice
		if price == 0 {
			price = line.Price
		}
		for _, a := range line.Items {
			count := 1
			for _, d := range a.Decorators {
				if d.Type == "QUANTITY" && d.Quantity > 0 {
					count = d.Quantity
				}
			}
			c.Lines = append(c.Lines, CartLine{
				ProductID:    a.ID,
				Name:         a.Name,
				UnitQuantity: a.UnitQuantity,
				Count:        count,
				Price:        int(price),
			})
		}
	}
	return c
}

// Cart returns the basket.
func (c *Client) Cart(ctx context.Context) (*Cart, error) {
	const path = "/cart"
	status, body, err := c.call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var r rawCart
	if err := decode(path, status, body, &r); err != nil {
		return nil, err
	}
	return r.cart(), nil
}

// AddProduct adds count of a product to the basket and returns the basket
// as it is afterwards. The ID is a Product's ID from Search.
func (c *Client) AddProduct(ctx context.Context, productID string, count int) (*Cart, error) {
	const path = "/cart/add_product"
	in := map[string]any{"product_id": productID, "count": count}
	status, body, err := c.call(ctx, http.MethodPost, path, in)
	if err != nil {
		return nil, err
	}
	var r rawCart
	if err := decode(path, status, body, &r); err != nil {
		return nil, err
	}
	return r.cart(), nil
}

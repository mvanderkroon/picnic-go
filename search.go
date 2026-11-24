package picnic

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"slices"
)

// Product is one product in search results.
type Product struct {
	ID              string // the ID to add to the basket, e.g. "s1019822"
	Name            string
	Price           int    // in euro cents, as shown on the tile
	UnitQuantity    string // the pack size as Picnic writes it, e.g. "1 kilo", "6 stuks"
	UnitQuantitySub string // a second line under it, e.g. a price per kilo
	MaxCount        int    // the most the basket accepts; 0 if not stated
	ImageID         string
}

// Search returns the products Picnic finds for a search term, in the order
// they appear on the results page, each once. A search with no results returns an empty slice; a response this
// package cannot read is an *Error.
func (c *Client) Search(ctx context.Context, term string) ([]Product, error) {
	path := "/pages/search-page-results?search_term=" + url.QueryEscape(term)
	status, body, err := c.call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	// The page is a tree of layout widgets whose shape varies by country
	// and experiment, so products are found by walking it for tiles rather
	// than by fixed paths.
	var page struct {
		Body map[string]any `json:"body"`
	}
	if err := decode(path, status, body, &page); err != nil {
		return nil, err
	}
	if page.Body == nil {
		return nil, &Error{Endpoint: endpoint(path), Status: status, Message: "unexpected response: no page body"}
	}
	products := []Product{}
	seen := map[string]bool{}
	for _, n := range walk(page.Body["child"]) {
		if n["type"] != "SELLING_UNIT_TILE" {
			continue
		}
		raw, ok := n["sellingUnit"]
		if !ok {
			continue
		}
		var u struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			DisplayPrice    cents  `json:"display_price"`
			UnitQuantity    string `json:"unit_quantity"`
			UnitQuantitySub string `json:"unit_quantity_sub"`
			MaxCount        int    `json:"max_count"`
			ImageID         string `json:"image_id"`
		}
		b, _ := json.Marshal(raw)
		if json.Unmarshal(b, &u) != nil || u.ID == "" || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		products = append(products, Product{
			ID:              u.ID,
			Name:            u.Name,
			Price:           int(u.DisplayPrice),
			UnitQuantity:    u.UnitQuantity,
			UnitQuantitySub: u.UnitQuantitySub,
			MaxCount:        u.MaxCount,
			ImageID:         u.ImageID,
		})
	}
	return products, nil
}

// walk returns every object in v, depth first, including objects inside
// arrays. Arrays keep their order; an object's keys are visited sorted, so
// the result is the same on every call.
func walk(v any) []map[string]any {
	var out []map[string]any
	var visit func(any)
	visit = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			out = append(out, t)
			for _, k := range slices.Sorted(maps.Keys(t)) {
				visit(t[k])
			}
		case []any:
			for _, child := range t {
				visit(child)
			}
		}
	}
	visit(v)
	return out
}

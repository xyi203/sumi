package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type categoryNode struct {
	ID       int64          `json:"id"`
	Name     string         `json:"name"`
	Type     int16          `json:"type"`
	Level    int16          `json:"level"`
	Children []categoryNode `json:"children"`
}

func (c *Client) ListCategories(ctx context.Context, rawType string) ([]byte, error) {
	billType, err := ParseBillType(rawType)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("type", strconv.Itoa(int(billType)))
	return c.Do(ctx, "GET", "/api/categories/", query, nil)
}

// AddCategory creates a second-level category; the API has no way to create a
// first-level one.
func (c *Client) AddCategory(ctx context.Context, name, parent, rawType string) ([]byte, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	if strings.TrimSpace(parent) == "" {
		return nil, fmt.Errorf("parent is required: an existing top-level category name")
	}
	billType, err := ParseBillType(rawType)
	if err != nil {
		return nil, err
	}
	return c.Do(ctx, "POST", "/api/categories/", nil, map[string]any{
		"name":        strings.TrimSpace(name),
		"type":        billType,
		"parent_name": strings.TrimSpace(parent),
	})
}

// resolveCategoryIDByName exists because listing transactions filters by
// category_id only, while callers work in names. billType 0 means "unknown", in
// which case both trees are searched and an ambiguous name is reported rather
// than guessed.
func (c *Client) resolveCategoryIDByName(ctx context.Context, name string, billType int16) (int64, error) {
	types := []int16{1, 2}
	if billType != 0 {
		types = []int16{billType}
	}

	matches := make([]categoryNode, 0, 2)
	for _, categoryType := range types {
		raw, err := c.ListCategories(ctx, strconv.Itoa(int(categoryType)))
		if err != nil {
			return 0, err
		}

		var tree []categoryNode
		if err := json.Unmarshal(raw, &tree); err != nil {
			return 0, fmt.Errorf("cannot parse category tree: %w", err)
		}
		for _, parent := range tree {
			for _, child := range parent.Children {
				if child.Level == 2 && strings.EqualFold(strings.TrimSpace(child.Name), name) {
					matches = append(matches, child)
				}
			}
		}
	}

	switch len(matches) {
	case 0:
		return 0, fmt.Errorf("category %q not found; list the valid names first", name)
	case 1:
		return matches[0].ID, nil
	default:
		return 0, fmt.Errorf("category %q exists for both expense and income; pass a type to disambiguate", name)
	}
}

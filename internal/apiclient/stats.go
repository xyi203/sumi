package apiclient

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// statsReports maps a report name onto its endpoint. Only "category" takes a
// type, the other two cover expense and income together.
var statsReports = map[string]string{
	"monthly":  "/api/stats/monthly",
	"daily":    "/api/stats/daily",
	"category": "/api/stats/category",
}

// GetStats fetches one report for a month (YYYY-MM, empty for the current one).
func (c *Client) GetStats(ctx context.Context, report, month, rawType string) ([]byte, error) {
	path, ok := statsReports[report]
	if !ok {
		return nil, fmt.Errorf("unknown report %q: use monthly, daily or category", report)
	}

	query := url.Values{}
	setIfPresent(query, "month", month)
	if report == "category" {
		billType, err := ParseBillType(orExpense(rawType))
		if err != nil {
			return nil, err
		}
		query.Set("type", strconv.Itoa(int(billType)))
	}
	return c.Do(ctx, "GET", path, query, nil)
}

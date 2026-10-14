package googlesheets

import (
	"context"
	"errors"
	"fmt"
	"google.golang.org/api/googleapi"
	sheets "google.golang.org/api/sheets/v4"
)

// Grid returns a bounded, uncached snapshot including typed values and formulas.
func (c *Client) Grid(ctx context.Context, id, rangeName string) (*sheets.Spreadsheet, error) {
	if id == "" || rangeName == "" {
		return nil, errors.New("spreadsheet ID and range are required")
	}
	result, err := c.sheets.Spreadsheets.Get(id).Ranges(rangeName).IncludeGridData(true).Fields(googleapi.Field("spreadsheetId,properties(title),sheets(properties(sheetId,title,gridProperties),data(startRow,startColumn,rowData(values(userEnteredValue,effectiveValue,formattedValue,effectiveFormat(numberFormat)))))")).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get spreadsheet grid: %w", err)
	}
	return result, nil
}

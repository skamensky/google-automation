package googlesheets

import (
	"context"
	"google.golang.org/api/option"
	sheets "google.golang.org/api/sheets/v4"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGridPreservesTypedEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ranges") != "'Data'!A1:C10" || r.URL.Query().Get("includeGridData") != "true" || r.URL.Query().Get("fields") == "" {
			t.Errorf("missing bounded snapshot parameters: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sheets":[{"data":[{"rowData":[{"values":[{"userEnteredValue":{"formulaValue":"=1+1"},"effectiveValue":{"numberValue":2},"formattedValue":"2"}]}]}]}]}`))
	}))
	defer server.Close()
	service, err := sheets.NewService(context.Background(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{sheets: service}
	v, err := c.Grid(context.Background(), "fixture", "'Data'!A1:C10")
	if err != nil {
		t.Fatal(err)
	}
	cell := v.Sheets[0].Data[0].RowData[0].Values[0]
	if cell.UserEnteredValue.FormulaValue == nil || *cell.UserEnteredValue.FormulaValue != "=1+1" || cell.EffectiveValue.NumberValue == nil || *cell.EffectiveValue.NumberValue != 2 {
		t.Fatal("typed formula evidence lost")
	}
	if _, err := c.Grid(context.Background(), "fixture", ""); err == nil {
		t.Fatal("unbounded snapshot accepted")
	}
}

package googleaccounts

import (
	"slices"
	"testing"

	"github.com/skamensky/google-automation/internal/googlesheets"
	sheets "google.golang.org/api/sheets/v4"
)

func TestLoginAuthorizesConnectedSheetsRefresh(t *testing.T) {
	loginScopes := Scopes()
	for _, required := range []string{sheets.SpreadsheetsScope, "https://www.googleapis.com/auth/bigquery.readonly"} {
		if !slices.Contains(loginScopes, required) || !slices.Contains(googlesheets.Scopes(), required) {
			t.Errorf("login and Sheets authorization must both request %s", required)
		}
	}
	if slices.Contains(loginScopes, "https://www.googleapis.com/auth/bigquery") {
		t.Fatal("Connected Sheets refresh must not require full BigQuery access")
	}
}

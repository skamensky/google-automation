package googleauth

import (
	"context"
	"encoding/json"
	"golang.org/x/oauth2"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTemporaryTokenRejectsExpiredAndRefreshCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "temporary.json")
	t.Setenv("GOOGLE_AUTOMATION_ACCESS_TOKEN_FILE", path)
	cases := []struct {
		token oauth2.Token
		valid bool
	}{
		{oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)}, true},
		{oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(-time.Hour)}, false},
		{oauth2.Token{AccessToken: "test", RefreshToken: "refresh", Expiry: time.Now().Add(time.Hour)}, false},
	}
	for _, tc := range cases {
		b, _ := json.Marshal(tc.token)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		_, err := NewClient(context.Background(), Config{})
		if (err == nil) != tc.valid {
			t.Fatalf("valid=%v error=%v", tc.valid, err)
		}
	}
}

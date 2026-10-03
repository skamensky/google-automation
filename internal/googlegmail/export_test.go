package googlegmail

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	gmail "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func TestConcurrentExportPreservesMessageIdentityAndRepairsResume(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		id := filepath.Base(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gmail.Message{Id: id, Snippet: strings.Repeat(id, 200)})
	}))
	defer server.Close()
	svc, err := gmail.NewService(context.Background(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{service: svc}
	dir := t.TempDir()
	items := []MessageListItem{}
	for i := 0; i < 80; i++ {
		items = append(items, MessageListItem{ID: fmt.Sprintf("message-%d", i)})
	}
	if err = os.WriteFile(filepath.Join(dir, "message-0.json"), []byte(`{"id":"wrong-message"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = c.exportMessageItems(context.Background(), items, dir, 8, "test"); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		b, e := os.ReadFile(filepath.Join(dir, item.ID+".json"))
		if e != nil {
			t.Fatal(e)
		}
		var m gmail.Message
		if e = json.Unmarshal(b, &m); e != nil || m.Id != item.ID || m.Snippet != strings.Repeat(item.ID, 200) {
			t.Fatal("export corrupted message identity", item.ID, e)
		}
	}
	before := calls.Load()
	if err = c.exportMessageItems(context.Background(), items, dir, 8, "test"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Fatal("verified resume redownloaded complete messages")
	}
}

package googlegmail

import (
	"context"
	"fmt"
	gmail "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListMessagesExhaustsPagesAndRespectsLimit(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		if r.URL.Query().Get("maxResults") == "" {
			t.Error("missing page size")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pageToken") == "" {
			fmt.Fprint(w, `{"messages":[{"id":"a","threadId":"t"}],"nextPageToken":"next"}`)
		} else {
			fmt.Fprint(w, `{"messages":[{"id":"b","threadId":"t"}]}`)
		}
	}))
	defer server.Close()
	svc, e := gmail.NewService(context.Background(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	if e != nil {
		t.Fatal(e)
	}
	c := &Client{service: svc}
	items, e := c.ListMessages(context.Background(), "has:attachment", nil, 1000, true)
	if e != nil || len(items) != 2 || pages != 2 {
		t.Fatalf("pagination failed: %d pages %d items %v", pages, len(items), e)
	}
	pages = 0
	items, e = c.ListMessages(context.Background(), "", nil, 1, false)
	if e != nil || len(items) != 1 || pages != 1 {
		t.Fatal("limit not respected")
	}
}
func TestWalkPartsIncludesInlineNamedAttachments(t *testing.T) {
	var out []AttachmentInfo
	walkParts("message", &gmail.MessagePart{Filename: "certificate.txt", Body: &gmail.MessagePartBody{Data: "aGVsbG8", Size: 5}}, &out)
	if len(out) != 1 || out[0].InlineData == "" {
		t.Fatal("inline attachment lost")
	}
}

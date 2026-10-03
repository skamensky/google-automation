package googlegmail

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	gmail "google.golang.org/api/gmail/v1"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// ExportMessages exhausts a search before downloading full messages. Completed
// message files are reusable on resumption; attachments remain API references.
func (c *Client) ExportMessages(ctx context.Context, query, dir string, workers int, includeSpamTrash bool) error {
	if workers < 1 || workers > 16 {
		return fmt.Errorf("workers must be 1..16")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	items, err := c.ListMessages(ctx, query, nil, 1000000, includeSpamTrash)
	if err != nil {
		return err
	}
	return c.exportMessageItems(ctx, items, dir, workers, query)
}
func (c *Client) ExportMessageIDs(ctx context.Context, input, dir string, workers int) error {
	raw, e := os.ReadFile(input)
	if e != nil {
		return e
	}
	var items []MessageListItem
	if e = json.Unmarshal(raw, &items); e != nil {
		return e
	}
	for _, item := range items {
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`).MatchString(item.ID) {
			return fmt.Errorf("invalid message ID")
		}
	}
	if workers < 1 || workers > 16 {
		return fmt.Errorf("workers must be 1..16")
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	digest := sha256.Sum256(raw)
	return c.exportMessageItems(ctx, items, dir, workers, "previous-search IDs "+hex.EncodeToString(digest[:]))
}
func (c *Client) exportMessageItems(ctx context.Context, items []MessageListItem, dir string, workers int, query string) error {
	var err error
	b, _ := json.MarshalIndent(map[string]any{"query": query, "messages": items, "complete": true}, "", "  ")
	if err = os.WriteFile(filepath.Join(dir, "inventory.json"), b, 0600); err != nil {
		return err
	}
	ledger := filepath.Join(dir, "queries")
	if err = os.MkdirAll(ledger, 0700); err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(query))
	if err = os.WriteFile(filepath.Join(ledger, hex.EncodeToString(digest[:])+".json"), b, 0600); err != nil {
		return err
	}
	jobs := make(chan MessageListItem)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	done := 0
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				path := filepath.Join(dir, item.ID+".json")
				if raw, e := os.ReadFile(path); e == nil {
					var saved gmail.Message
					if json.Unmarshal(raw, &saved) == nil && saved.Id == item.ID {
						continue
					}
				}
				message, e := c.GetMessage(ctx, item.ID, "full")
				if e == nil {
					var data []byte
					data, e = json.Marshal(message)
					if e == nil {
						e = os.WriteFile(path+".part", data, 0600)
						if e == nil {
							e = os.Rename(path+".part", path)
						}
					}
				}
				mu.Lock()
				if e != nil && first == nil {
					first = e
				}
				done++
				if done%100 == 0 {
					fmt.Fprintf(os.Stderr, "Exported %d / %d messages\n", done, len(items))
				}
				mu.Unlock()
			}
		}()
	}
	for _, item := range items {
		select {
		case jobs <- item:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return first
}

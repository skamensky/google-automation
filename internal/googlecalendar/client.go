package googlecalendar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/skamensky/google-automation/internal/cache"
	"github.com/skamensky/google-automation/internal/googleauth"
	calendar "google.golang.org/api/calendar/v3"
)

type Config struct {
	CredentialsFile string
	TokenFile       string
	NoBrowser       bool
	Cache           cache.Config
}

type Client struct {
	service *calendar.Service
}

type ListCalendarsOptions struct{}

type CreateEventOptions struct {
	CalendarID  string
	Summary     string
	Start       string
	End         string
	TimeZone    string
	Attendees   []string
	SendUpdates string
}

type ListEventsOptions struct {
	CalendarID   string
	TimeMin      string
	TimeMax      string
	Query        string
	Attendees    []string
	MaxResults   int64
	ShowDeleted  bool
	SingleEvents bool
	OrderBy      string
}

type GetEventOptions struct {
	CalendarID string
	EventID    string
}

type DeleteEventOptions struct {
	CalendarID  string
	EventID     string
	SendUpdates string
}

type EventFieldUpdate struct {
	Field string
	Value string
}

type UpdateEventOptions struct {
	CalendarID  string
	EventID     string
	Fields      []EventFieldUpdate
	TimeZone    string
	SendUpdates string
}

type FreeBusyOptions struct {
	TimeMin     string
	TimeMax     string
	TimeZone    string
	CalendarIDs []string
}

func Scopes() []string {
	return []string{calendar.CalendarScope}
}

func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	httpClient, err := googleauth.NewClient(ctx, googleauth.Config{
		CredentialsFile: cfg.CredentialsFile,
		TokenFile:       cfg.TokenFile,
		Scopes:          Scopes(),
		NoBrowser:       cfg.NoBrowser,
	})
	if err != nil {
		return nil, err
	}

	service, err := calendar.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("create calendar service: %w", err)
	}

	return &Client{service: service}, nil
}

func (c *Client) ListCalendars(ctx context.Context, _ ListCalendarsOptions) ([]*calendar.CalendarListEntry, error) {
	var calendars []*calendar.CalendarListEntry
	var pageToken string
	for {
		call := c.service.CalendarList.List().Context(ctx)
		if pageToken != "" {
			call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("list calendars: %w", err)
		}
		calendars = append(calendars, resp.Items...)
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return calendars, nil
}

func (c *Client) CreateEvent(ctx context.Context, opts CreateEventOptions) (*calendar.Event, error) {
	if opts.CalendarID == "" {
		opts.CalendarID = "primary"
	}
	if opts.Summary == "" {
		return nil, fmt.Errorf("summary is required")
	}
	if opts.Start == "" {
		return nil, fmt.Errorf("start is required")
	}
	if opts.End == "" {
		return nil, fmt.Errorf("end is required")
	}
	if opts.TimeZone == "" {
		opts.TimeZone = "Asia/Jerusalem"
	}
	if opts.SendUpdates == "" {
		opts.SendUpdates = "all"
	}

	attendees := make([]*calendar.EventAttendee, 0, len(opts.Attendees))
	for _, email := range opts.Attendees {
		if email == "" {
			continue
		}
		attendees = append(attendees, &calendar.EventAttendee{Email: email})
	}

	event := &calendar.Event{
		Summary: opts.Summary,
		Start: &calendar.EventDateTime{
			DateTime: opts.Start,
			TimeZone: opts.TimeZone,
		},
		End: &calendar.EventDateTime{
			DateTime: opts.End,
			TimeZone: opts.TimeZone,
		},
		Attendees: attendees,
	}

	created, err := c.service.Events.Insert(opts.CalendarID, event).
		SendUpdates(opts.SendUpdates).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("create calendar event: %w", err)
	}
	return created, nil
}

func (c *Client) ListEvents(ctx context.Context, opts ListEventsOptions) ([]*calendar.Event, error) {
	if opts.CalendarID == "" {
		opts.CalendarID = "primary"
	}
	if opts.MaxResults <= 0 {
		opts.MaxResults = 50
	}
	if !opts.SingleEvents {
		opts.SingleEvents = true
	}
	if opts.OrderBy == "" {
		opts.OrderBy = "startTime"
	}

	var events []*calendar.Event
	var pageToken string
	for {
		call := c.service.Events.List(opts.CalendarID).
			MaxResults(opts.MaxResults).
			ShowDeleted(opts.ShowDeleted).
			SingleEvents(opts.SingleEvents).
			Context(ctx)
		if opts.OrderBy != "" {
			call.OrderBy(opts.OrderBy)
		}
		if opts.TimeMin != "" {
			call.TimeMin(opts.TimeMin)
		}
		if opts.TimeMax != "" {
			call.TimeMax(opts.TimeMax)
		}
		if opts.Query != "" {
			call.Q(opts.Query)
		}
		if pageToken != "" {
			call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("list calendar events: %w", err)
		}
		for _, event := range resp.Items {
			if eventMatchesAttendees(event, opts.Attendees) {
				events = append(events, event)
			}
		}
		if resp.NextPageToken == "" || int64(len(events)) >= opts.MaxResults {
			break
		}
		pageToken = resp.NextPageToken
	}
	if int64(len(events)) > opts.MaxResults {
		events = events[:opts.MaxResults]
	}
	return events, nil
}

func (c *Client) GetEvent(ctx context.Context, opts GetEventOptions) (*calendar.Event, error) {
	if opts.CalendarID == "" {
		opts.CalendarID = "primary"
	}
	if opts.EventID == "" {
		return nil, fmt.Errorf("event ID is required")
	}

	event, err := c.service.Events.Get(opts.CalendarID, opts.EventID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get calendar event %q: %w", opts.EventID, err)
	}
	return event, nil
}

func (c *Client) UpdateEvent(ctx context.Context, opts UpdateEventOptions) (*calendar.Event, error) {
	if opts.CalendarID == "" {
		opts.CalendarID = "primary"
	}
	if opts.EventID == "" {
		return nil, fmt.Errorf("event ID is required")
	}
	if opts.TimeZone == "" {
		opts.TimeZone = "Asia/Jerusalem"
	}
	if opts.SendUpdates == "" {
		opts.SendUpdates = "all"
	}
	if len(opts.Fields) == 0 {
		return nil, fmt.Errorf("at least one --field is required")
	}

	event, err := c.GetEvent(ctx, GetEventOptions{CalendarID: opts.CalendarID, EventID: opts.EventID})
	if err != nil {
		return nil, err
	}
	for _, field := range opts.Fields {
		if err := applyEventFieldUpdate(event, field, opts.TimeZone); err != nil {
			return nil, err
		}
	}

	updated, err := c.service.Events.Update(opts.CalendarID, opts.EventID, event).
		SendUpdates(opts.SendUpdates).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("update calendar event %q: %w", opts.EventID, err)
	}
	return updated, nil
}

func (c *Client) DeleteEvent(ctx context.Context, opts DeleteEventOptions) error {
	if opts.CalendarID == "" {
		opts.CalendarID = "primary"
	}
	if opts.EventID == "" {
		return fmt.Errorf("event ID is required")
	}
	if opts.SendUpdates == "" {
		opts.SendUpdates = "all"
	}

	if err := c.service.Events.Delete(opts.CalendarID, opts.EventID).
		SendUpdates(opts.SendUpdates).
		Context(ctx).
		Do(); err != nil {
		return fmt.Errorf("delete calendar event %q: %w", opts.EventID, err)
	}
	return nil
}

func (c *Client) FreeBusy(ctx context.Context, opts FreeBusyOptions) (*calendar.FreeBusyResponse, error) {
	if opts.TimeMin == "" {
		return nil, fmt.Errorf("time min is required")
	}
	if opts.TimeMax == "" {
		return nil, fmt.Errorf("time max is required")
	}
	if opts.TimeZone == "" {
		opts.TimeZone = "Asia/Jerusalem"
	}
	if len(opts.CalendarIDs) == 0 {
		opts.CalendarIDs = []string{"primary"}
	}

	items := make([]*calendar.FreeBusyRequestItem, 0, len(opts.CalendarIDs))
	for _, id := range opts.CalendarIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			items = append(items, &calendar.FreeBusyRequestItem{Id: id})
		}
	}
	req := &calendar.FreeBusyRequest{
		TimeMin:  opts.TimeMin,
		TimeMax:  opts.TimeMax,
		TimeZone: opts.TimeZone,
		Items:    items,
	}
	resp, err := c.service.Freebusy.Query(req).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("query freebusy: %w", err)
	}
	return resp, nil
}

func SupportedEventUpdateFields() []string {
	fields := make([]string, 0, len(eventUpdateFieldSet))
	for field := range eventUpdateFieldSet {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func ParseEventFieldUpdate(raw string) (EventFieldUpdate, error) {
	field, value, ok := strings.Cut(strings.TrimSpace(raw), "=")
	if !ok {
		return EventFieldUpdate{}, fmt.Errorf("field update %q must use name=value", raw)
	}
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if field == "" || value == "" {
		return EventFieldUpdate{}, fmt.Errorf("field update %q must include a non-empty name and value", raw)
	}
	if _, ok := eventUpdateFieldSet[field]; !ok {
		return EventFieldUpdate{}, fmt.Errorf("unsupported update field %q; supported fields: %s", field, strings.Join(SupportedEventUpdateFields(), ","))
	}
	return EventFieldUpdate{Field: field, Value: value}, nil
}

func WriteJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

var eventUpdateFieldSet = map[string]struct{}{
	"attendee":    {},
	"description": {},
	"end":         {},
	"location":    {},
	"start":       {},
	"summary":     {},
}

func applyEventFieldUpdate(event *calendar.Event, field EventFieldUpdate, timezone string) error {
	switch field.Field {
	case "summary":
		event.Summary = field.Value
	case "description":
		event.Description = field.Value
	case "location":
		event.Location = field.Value
	case "start":
		event.Start = &calendar.EventDateTime{DateTime: field.Value, TimeZone: timezone}
	case "end":
		event.End = &calendar.EventDateTime{DateTime: field.Value, TimeZone: timezone}
	case "attendee":
		if !hasAttendee(event.Attendees, field.Value) {
			event.Attendees = append(event.Attendees, &calendar.EventAttendee{Email: field.Value})
		}
	default:
		return fmt.Errorf("unsupported update field %q; supported fields: %s", field.Field, strings.Join(SupportedEventUpdateFields(), ","))
	}
	return nil
}

func hasAttendee(attendees []*calendar.EventAttendee, email string) bool {
	for _, attendee := range attendees {
		if attendee != nil && strings.EqualFold(strings.TrimSpace(attendee.Email), strings.TrimSpace(email)) {
			return true
		}
	}
	return false
}

func eventMatchesAttendees(event *calendar.Event, attendees []string) bool {
	if len(attendees) == 0 {
		return true
	}
	wanted := make(map[string]struct{}, len(attendees))
	for _, attendee := range attendees {
		attendee = strings.ToLower(strings.TrimSpace(attendee))
		if attendee != "" {
			wanted[attendee] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return true
	}
	for _, attendee := range event.Attendees {
		if attendee == nil {
			continue
		}
		if _, ok := wanted[strings.ToLower(strings.TrimSpace(attendee.Email))]; ok {
			return true
		}
	}
	return false
}

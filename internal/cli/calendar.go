package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/skamensky/google-automation/internal/googlecalendar"
	"github.com/spf13/cobra"
)

func newCalendarCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "Manage Google Calendar",
	}

	cmd.AddCommand(newCalendarCalendarsCommand(cfg))
	cmd.AddCommand(newCalendarEventsCommand(cfg))
	cmd.AddCommand(newCalendarFreeBusyCommand(cfg))
	return cmd
}

func newCalendarCalendarsCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendars",
		Short: "Manage Google Calendar lists",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List accessible calendars",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := commandContext(cmd)
			client, err := googlecalendar.NewClient(ctx, cfg.calendarConfig())
			if err != nil {
				return err
			}
			calendars, err := client.ListCalendars(ctx, googlecalendar.ListCalendarsOptions{})
			if err != nil {
				return err
			}
			return googlecalendar.WriteJSON(cmd.OutOrStdout(), calendars)
		},
	})
	return cmd
}

func newCalendarEventsCommand(cfg *appConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Manage Google Calendar events",
	}

	cmd.AddCommand(newCalendarEventsCreateCommand(cfg))
	cmd.AddCommand(newCalendarEventsListCommand(cfg))
	cmd.AddCommand(newCalendarEventsSearchCommand(cfg))
	cmd.AddCommand(newCalendarEventsGetCommand(cfg))
	cmd.AddCommand(newCalendarEventsUpdateCommand(cfg))
	cmd.AddCommand(newCalendarEventsDeleteCommand(cfg))
	return cmd
}

func newCalendarEventsCreateCommand(cfg *appConfig) *cobra.Command {
	var calendarID string
	var summary string
	var start string
	var end string
	var timezone string
	var attendees []string
	var sendUpdates string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Google Calendar event",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := commandContext(cmd)

			startRFC3339, err := parseCalendarDateTime(start, timezone)
			if err != nil {
				return fmt.Errorf("parse start: %w", err)
			}
			endRFC3339, err := parseCalendarDateTime(end, timezone)
			if err != nil {
				return fmt.Errorf("parse end: %w", err)
			}

			client, err := googlecalendar.NewClient(ctx, cfg.calendarConfig())
			if err != nil {
				return err
			}

			event, err := client.CreateEvent(ctx, googlecalendar.CreateEventOptions{
				CalendarID:  calendarID,
				Summary:     summary,
				Start:       startRFC3339,
				End:         endRFC3339,
				TimeZone:    timezone,
				Attendees:   attendees,
				SendUpdates: sendUpdates,
			})
			if err != nil {
				return err
			}

			return googlecalendar.WriteJSON(cmd.OutOrStdout(), event)
		},
	}

	addEventMutationFlags(cmd, &calendarID, &timezone, &sendUpdates)
	cmd.Flags().StringVar(&summary, "summary", "", "event title")
	cmd.Flags().StringVar(&start, "start", "", "event start time, RFC3339 or YYYY-MM-DD HH:MM")
	cmd.Flags().StringVar(&end, "end", "", "event end time, RFC3339 or YYYY-MM-DD HH:MM")
	cmd.Flags().StringArrayVar(&attendees, "attendee", nil, "attendee email address; repeat for multiple attendees")
	_ = cmd.MarkFlagRequired("summary")
	_ = cmd.MarkFlagRequired("start")
	_ = cmd.MarkFlagRequired("end")

	return cmd
}

func newCalendarEventsListCommand(cfg *appConfig) *cobra.Command {
	var opts calendarRangeFlags
	var calendarID string
	var query string
	var attendees []string
	var limit int64

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Google Calendar events",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := commandContext(cmd)
			timeMin, timeMax, err := opts.rangeValues()
			if err != nil {
				return err
			}
			client, err := googlecalendar.NewClient(ctx, cfg.calendarConfig())
			if err != nil {
				return err
			}
			events, err := client.ListEvents(ctx, googlecalendar.ListEventsOptions{
				CalendarID: calendarID,
				TimeMin:    timeMin,
				TimeMax:    timeMax,
				Query:      query,
				Attendees:  attendees,
				MaxResults: limit,
			})
			if err != nil {
				return err
			}
			return googlecalendar.WriteJSON(cmd.OutOrStdout(), events)
		},
	}
	addCalendarRangeFlags(cmd, &opts)
	cmd.Flags().StringVar(&calendarID, "calendar-id", "primary", "calendar ID")
	cmd.Flags().StringVar(&query, "query", "", "Calendar API full-text query")
	cmd.Flags().StringArrayVar(&attendees, "attendee", nil, "filter to events containing attendee; repeat for multiple")
	cmd.Flags().Int64Var(&limit, "limit", 50, "maximum events to return")
	return cmd
}

func newCalendarEventsSearchCommand(cfg *appConfig) *cobra.Command {
	var opts calendarRangeFlags
	var calendarID string
	var attendees []string
	var limit int64

	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: "Search Google Calendar events",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := commandContext(cmd)
			timeMin, timeMax, err := opts.rangeValues()
			if err != nil {
				return err
			}
			client, err := googlecalendar.NewClient(ctx, cfg.calendarConfig())
			if err != nil {
				return err
			}
			events, err := client.ListEvents(ctx, googlecalendar.ListEventsOptions{
				CalendarID: calendarID,
				TimeMin:    timeMin,
				TimeMax:    timeMax,
				Query:      args[0],
				Attendees:  attendees,
				MaxResults: limit,
			})
			if err != nil {
				return err
			}
			return googlecalendar.WriteJSON(cmd.OutOrStdout(), events)
		},
	}
	addCalendarRangeFlags(cmd, &opts)
	cmd.Flags().StringVar(&calendarID, "calendar-id", "primary", "calendar ID")
	cmd.Flags().StringArrayVar(&attendees, "attendee", nil, "filter to events containing attendee; repeat for multiple")
	cmd.Flags().Int64Var(&limit, "limit", 50, "maximum events to return")
	return cmd
}

func newCalendarEventsGetCommand(cfg *appConfig) *cobra.Command {
	var calendarID string
	cmd := &cobra.Command{
		Use:   "get EVENT_ID",
		Short: "Get a Google Calendar event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := commandContext(cmd)
			client, err := googlecalendar.NewClient(ctx, cfg.calendarConfig())
			if err != nil {
				return err
			}
			event, err := client.GetEvent(ctx, googlecalendar.GetEventOptions{
				CalendarID: calendarID,
				EventID:    args[0],
			})
			if err != nil {
				return err
			}
			return googlecalendar.WriteJSON(cmd.OutOrStdout(), event)
		},
	}
	cmd.Flags().StringVar(&calendarID, "calendar-id", "primary", "calendar ID")
	return cmd
}

func newCalendarEventsUpdateCommand(cfg *appConfig) *cobra.Command {
	var calendarID string
	var timezone string
	var sendUpdates string
	var rawFields []string

	cmd := &cobra.Command{
		Use:   "update EVENT_ID --field FIELD=VALUE",
		Short: "Update a Google Calendar event",
		Long:  "Update a Google Calendar event. Repeat --field for multiple updates. Supported fields: summary, description, location, start, end, attendee.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := commandContext(cmd)
			fields := make([]googlecalendar.EventFieldUpdate, 0, len(rawFields))
			for _, raw := range rawFields {
				field, err := parseCalendarEventFieldUpdate(raw, timezone)
				if err != nil {
					return err
				}
				fields = append(fields, field)
			}
			client, err := googlecalendar.NewClient(ctx, cfg.calendarConfig())
			if err != nil {
				return err
			}
			event, err := client.UpdateEvent(ctx, googlecalendar.UpdateEventOptions{
				CalendarID:  calendarID,
				EventID:     args[0],
				Fields:      fields,
				TimeZone:    timezone,
				SendUpdates: sendUpdates,
			})
			if err != nil {
				return err
			}
			return googlecalendar.WriteJSON(cmd.OutOrStdout(), event)
		},
	}
	addEventMutationFlags(cmd, &calendarID, &timezone, &sendUpdates)
	cmd.Flags().StringArrayVar(&rawFields, "field", nil, "field update as name=value; repeat for multiple updates")
	_ = cmd.MarkFlagRequired("field")
	return cmd
}

func newCalendarEventsDeleteCommand(cfg *appConfig) *cobra.Command {
	var calendarID string
	var sendUpdates string
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete EVENT_ID --yes",
		Short: "Delete a Google Calendar event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("refusing to delete without --yes")
			}
			ctx := commandContext(cmd)
			client, err := googlecalendar.NewClient(ctx, cfg.calendarConfig())
			if err != nil {
				return err
			}
			if err := client.DeleteEvent(ctx, googlecalendar.DeleteEventOptions{
				CalendarID:  calendarID,
				EventID:     args[0],
				SendUpdates: sendUpdates,
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted event %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&calendarID, "calendar-id", "primary", "calendar ID")
	cmd.Flags().StringVar(&sendUpdates, "send-updates", "all", "Google Calendar sendUpdates value: all, externalOnly, or none")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm event deletion")
	return cmd
}

func newCalendarFreeBusyCommand(cfg *appConfig) *cobra.Command {
	var opts calendarRangeFlags
	var calendarIDs []string

	cmd := &cobra.Command{
		Use:   "freebusy",
		Short: "Query Google Calendar free/busy time",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := commandContext(cmd)
			timeMin, timeMax, err := opts.rangeValues()
			if err != nil {
				return err
			}
			if timeMin == "" || timeMax == "" {
				return fmt.Errorf("--from and --to are required for freebusy")
			}
			client, err := googlecalendar.NewClient(ctx, cfg.calendarConfig())
			if err != nil {
				return err
			}
			resp, err := client.FreeBusy(ctx, googlecalendar.FreeBusyOptions{
				TimeMin:     timeMin,
				TimeMax:     timeMax,
				TimeZone:    opts.timezone,
				CalendarIDs: calendarIDs,
			})
			if err != nil {
				return err
			}
			return googlecalendar.WriteJSON(cmd.OutOrStdout(), resp)
		},
	}
	addCalendarRangeFlags(cmd, &opts)
	cmd.Flags().StringArrayVar(&calendarIDs, "calendar-id", []string{"primary"}, "calendar ID to check; repeat for multiple")
	return cmd
}

type calendarRangeFlags struct {
	from     string
	to       string
	today    bool
	tomorrow bool
	timezone string
}

func addCalendarRangeFlags(cmd *cobra.Command, opts *calendarRangeFlags) {
	cmd.Flags().StringVar(&opts.from, "from", "", "range start, RFC3339 or YYYY-MM-DD HH:MM")
	cmd.Flags().StringVar(&opts.to, "to", "", "range end, RFC3339 or YYYY-MM-DD HH:MM")
	cmd.Flags().BoolVar(&opts.today, "today", false, "use today's local day as the range")
	cmd.Flags().BoolVar(&opts.tomorrow, "tomorrow", false, "use tomorrow's local day as the range")
	cmd.Flags().StringVar(&opts.timezone, "timezone", "Asia/Jerusalem", "IANA timezone for local date/time values")
}

func addEventMutationFlags(cmd *cobra.Command, calendarID, timezone, sendUpdates *string) {
	cmd.Flags().StringVar(calendarID, "calendar-id", "primary", "calendar ID")
	cmd.Flags().StringVar(timezone, "timezone", "Asia/Jerusalem", "IANA timezone for local date/time values")
	cmd.Flags().StringVar(sendUpdates, "send-updates", "all", "Google Calendar sendUpdates value: all, externalOnly, or none")
}

func (opts calendarRangeFlags) rangeValues() (string, string, error) {
	if opts.today && opts.tomorrow {
		return "", "", fmt.Errorf("--today and --tomorrow cannot both be set")
	}
	if opts.today || opts.tomorrow {
		location, err := time.LoadLocation(defaultCalendarTimezone(opts.timezone))
		if err != nil {
			return "", "", fmt.Errorf("load timezone %q: %w", opts.timezone, err)
		}
		now := time.Now().In(location)
		if opts.tomorrow {
			now = now.AddDate(0, 0, 1)
		}
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
		end := start.AddDate(0, 0, 1)
		return start.Format(time.RFC3339), end.Format(time.RFC3339), nil
	}

	from := ""
	to := ""
	var err error
	if strings.TrimSpace(opts.from) != "" {
		from, err = parseCalendarDateTime(opts.from, opts.timezone)
		if err != nil {
			return "", "", fmt.Errorf("parse from: %w", err)
		}
	}
	if strings.TrimSpace(opts.to) != "" {
		to, err = parseCalendarDateTime(opts.to, opts.timezone)
		if err != nil {
			return "", "", fmt.Errorf("parse to: %w", err)
		}
	}
	return from, to, nil
}

func commandContext(cmd *cobra.Command) context.Context {
	ctx := cmd.Context()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func parseCalendarEventFieldUpdate(raw, timezone string) (googlecalendar.EventFieldUpdate, error) {
	field, err := googlecalendar.ParseEventFieldUpdate(raw)
	if err != nil {
		return field, err
	}
	switch field.Field {
	case "start", "end":
		parsed, err := parseCalendarDateTime(field.Value, timezone)
		if err != nil {
			return field, fmt.Errorf("parse %s: %w", field.Field, err)
		}
		field.Value = parsed
	}
	return field, nil
}

func parseCalendarDateTime(value, timezone string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("value is required")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.Format(time.RFC3339), nil
	}
	timezone = defaultCalendarTimezone(timezone)
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", fmt.Errorf("load timezone %q: %w", timezone, err)
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
		parsed, err := time.ParseInLocation(layout, value, location)
		if err == nil {
			return parsed.Format(time.RFC3339), nil
		}
	}
	return "", fmt.Errorf("expected RFC3339, YYYY-MM-DD HH:MM, or YYYY-MM-DD")
}

func defaultCalendarTimezone(timezone string) string {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		return "Asia/Jerusalem"
	}
	return timezone
}

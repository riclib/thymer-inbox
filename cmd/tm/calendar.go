package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

const (
	calendarBucket     = "calendar_events"
	calendarMetaBucket = "calendar_meta"
)

// CalendarEvent represents a stored calendar event
type CalendarEvent struct {
	ID          string    `json:"id"`           // gcal_{eventId}
	CalendarID  string    `json:"calendar_id"`  // primary, work@company.com, etc.
	CalendarName string   `json:"calendar_name"` // Human-readable calendar name
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	AllDay      bool      `json:"all_day"`
	Attendees   []string  `json:"attendees"`
	MeetLink    string    `json:"meet_link"`
	Status      string    `json:"status"` // confirmed, tentative, cancelled
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Verb        string    `json:"-"` // transient: created, updated, cancelled (not stored)
}

// ToMarkdown returns the event as markdown with YAML frontmatter
func (e CalendarEvent) ToMarkdown() string {
	var b strings.Builder

	// YAML frontmatter
	b.WriteString("---\n")
	b.WriteString("collection: Calendar\n")
	b.WriteString(fmt.Sprintf("external_id: %s\n", e.ID))
	if e.Verb != "" {
		b.WriteString(fmt.Sprintf("verb: %s\n", e.Verb))
	}
	b.WriteString(fmt.Sprintf("title: %s\n", e.Title))
	// Normalize calendar name to match choice IDs
	calendarChoice := normalizeCalendarName(e.CalendarID, e.CalendarName)
	b.WriteString(fmt.Sprintf("calendar: %s\n", calendarChoice))
	b.WriteString(fmt.Sprintf("start: %d\n", e.Start.Unix()))
	b.WriteString(fmt.Sprintf("end: %d\n", e.End.Unix()))
	if e.AllDay {
		b.WriteString("all_day: true\n")
	}
	if e.Location != "" {
		b.WriteString(fmt.Sprintf("location: %s\n", e.Location))
	}
	if len(e.Attendees) > 0 {
		b.WriteString(fmt.Sprintf("attendees: %s\n", strings.Join(e.Attendees, ", ")))
	}
	if e.MeetLink != "" {
		b.WriteString(fmt.Sprintf("meet_link: %s\n", e.MeetLink))
	}
	b.WriteString(fmt.Sprintf("status: %s\n", e.Status))
	b.WriteString("---\n\n")

	// Body (description)
	if e.Description != "" {
		b.WriteString(e.Description)
	}

	return b.String()
}

// CalendarInfo represents a user's calendar
type CalendarInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Primary bool   `json:"primary"`
	Color   string `json:"color"`
}

// CalendarSyncer handles syncing Google Calendar events
type CalendarSyncer struct {
	service   *calendar.Service
	db        *bolt.DB
	calendars []string // Calendar IDs to sync
}

// CalendarTokens holds OAuth tokens for Google Calendar
type CalendarTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
}

// NewCalendarSyncer creates a new syncer
func NewCalendarSyncer(tokens *CalendarTokens, calendars []string, dataDir string) (*CalendarSyncer, error) {
	ctx := context.Background()

	// Get OAuth config with credentials
	oauthConfig := getGoogleOAuthConfig()

	// Create OAuth token source
	token := &oauth2.Token{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    tokens.TokenType,
		Expiry:       tokens.Expiry,
	}

	tokenSource := oauthConfig.TokenSource(ctx, token)

	// Create calendar service
	srv, err := calendar.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("failed to create calendar service: %w", err)
	}

	// Open bbolt database
	dbPath := filepath.Join(dataDir, "calendar.db")
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create buckets
	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(calendarBucket)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(calendarMetaBucket)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create buckets: %w", err)
	}

	return &CalendarSyncer{
		service:   srv,
		db:        db,
		calendars: calendars,
	}, nil
}

// Close closes the database
func (s *CalendarSyncer) Close() error {
	return s.db.Close()
}

// ClearCache clears all cached events from the database
func (s *CalendarSyncer) ClearCache() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("calendar_events"))
		if b == nil {
			return nil
		}

		var keysToDelete [][]byte
		b.ForEach(func(k, v []byte) error {
			keysToDelete = append(keysToDelete, k)
			return nil
		})

		for _, k := range keysToDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListCalendars returns all calendars accessible to the user
func (s *CalendarSyncer) ListCalendars(ctx context.Context) ([]CalendarInfo, error) {
	list, err := s.service.CalendarList.List().Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list calendars: %w", err)
	}

	var calendars []CalendarInfo
	for _, cal := range list.Items {
		calendars = append(calendars, CalendarInfo{
			ID:      cal.Id,
			Name:    cal.Summary,
			Primary: cal.Primary,
			Color:   cal.BackgroundColor,
		})
	}

	return calendars, nil
}

// CalendarSyncResult contains sync statistics
type CalendarSyncResult struct {
	Created   []CalendarEvent
	Updated   []CalendarEvent
	Cancelled []CalendarEvent
	Unchanged int
	Errors    []error
}

// Sync fetches events and returns changes
func (s *CalendarSyncer) Sync(ctx context.Context) (*CalendarSyncResult, error) {
	result := &CalendarSyncResult{
		Created:   make([]CalendarEvent, 0),
		Updated:   make([]CalendarEvent, 0),
		Cancelled: make([]CalendarEvent, 0),
		Errors:    make([]error, 0),
	}

	// Get calendar names for display
	calendarNames := make(map[string]string)
	if calendars, err := s.ListCalendars(ctx); err == nil {
		for _, cal := range calendars {
			calendarNames[cal.ID] = cal.Name
		}
	}

	for _, calendarID := range s.calendars {
		events, err := s.syncCalendar(ctx, calendarID, calendarNames[calendarID])
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to sync %s: %w", calendarID, err))
			continue
		}

		for _, event := range events {
			upsertResult, err := s.upsert(event)
			if err != nil {
				result.Errors = append(result.Errors, err)
				continue
			}

			event.Verb = upsertResult.Verb
			switch upsertResult.Action {
			case "created":
				result.Created = append(result.Created, event)
			case "updated":
				result.Updated = append(result.Updated, event)
			case "cancelled":
				result.Cancelled = append(result.Cancelled, event)
			case "unchanged":
				result.Unchanged++
			}
		}
	}

	return result, nil
}

func (s *CalendarSyncer) syncCalendar(ctx context.Context, calendarID, calendarName string) ([]CalendarEvent, error) {
	// Fetch events from 1 week ago to 12 weeks ahead
	now := time.Now()
	timeMin := now.AddDate(0, 0, -7).Format(time.RFC3339)  // 1 week back
	timeMax := now.AddDate(0, 0, 84).Format(time.RFC3339)  // 12 weeks (84 days) forward

	events, err := s.service.Events.List(calendarID).
		Context(ctx).
		TimeMin(timeMin).
		TimeMax(timeMax).
		SingleEvents(true).
		OrderBy("startTime").
		MaxResults(100).
		Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	logger.Info("calendar sync: fetched from Google",
		"calendar", calendarName,
		"raw_count", len(events.Items))

	var result []CalendarEvent
	for _, item := range events.Items {
		logger.Debug("calendar sync: raw event from Google",
			"google_id", item.Id,
			"title", item.Summary,
			"recurring_id", item.RecurringEventId)
		event := s.convertEvent(calendarID, calendarName, item)
		result = append(result, event)
	}

	return result, nil
}

func (s *CalendarSyncer) convertEvent(calendarID, calendarName string, item *calendar.Event) CalendarEvent {
	id := fmt.Sprintf("gcal_%s", item.Id)

	event := CalendarEvent{
		ID:           id,
		CalendarID:   calendarID,
		CalendarName: calendarName,
		Title:        item.Summary,
		Description:  item.Description,
		Location:     item.Location,
		Status:       item.Status,
	}

	// Parse start/end times
	if item.Start != nil {
		if item.Start.DateTime != "" {
			event.Start, _ = time.Parse(time.RFC3339, item.Start.DateTime)
		} else if item.Start.Date != "" {
			event.Start, _ = time.Parse("2006-01-02", item.Start.Date)
			event.AllDay = true
		}
	}
	if item.End != nil {
		if item.End.DateTime != "" {
			event.End, _ = time.Parse(time.RFC3339, item.End.DateTime)
		} else if item.End.Date != "" {
			event.End, _ = time.Parse("2006-01-02", item.End.Date)
		}
	}

	// Extract attendees
	for _, attendee := range item.Attendees {
		if attendee.DisplayName != "" {
			event.Attendees = append(event.Attendees, attendee.DisplayName)
		} else {
			event.Attendees = append(event.Attendees, attendee.Email)
		}
	}

	// Extract meeting link
	if item.HangoutLink != "" {
		event.MeetLink = item.HangoutLink
	} else if item.ConferenceData != nil {
		for _, ep := range item.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" {
				event.MeetLink = ep.Uri
				break
			}
		}
	}

	// Timestamps
	if item.Created != "" {
		event.CreatedAt, _ = time.Parse(time.RFC3339, item.Created)
	}
	if item.Updated != "" {
		event.UpdatedAt, _ = time.Parse(time.RFC3339, item.Updated)
	}

	return event
}

// CalendarUpsertResult contains the result of an upsert operation
type CalendarUpsertResult struct {
	Action string // created, updated, cancelled, unchanged
	Verb   string // created, updated, cancelled
}

func (s *CalendarSyncer) upsert(event CalendarEvent) (*CalendarUpsertResult, error) {
	result := &CalendarUpsertResult{}

	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(calendarBucket))

		existing := b.Get([]byte(event.ID))
		if existing == nil {
			// New event
			logger.Debug("calendar upsert: NEW event",
				"id", event.ID,
				"title", event.Title,
				"start", event.Start.Format(time.RFC3339))
			data, err := json.Marshal(event)
			if err != nil {
				return err
			}
			result.Action = "created"
			result.Verb = "created"
			return b.Put([]byte(event.ID), data)
		}

		// Check if changed
		var old CalendarEvent
		if err := json.Unmarshal(existing, &old); err != nil {
			return err
		}

		// Check for cancellation
		if event.Status == "cancelled" && old.Status != "cancelled" {
			logger.Debug("calendar upsert: CANCELLED",
				"id", event.ID,
				"title", event.Title)
			data, err := json.Marshal(event)
			if err != nil {
				return err
			}
			result.Action = "cancelled"
			result.Verb = "cancelled"
			return b.Put([]byte(event.ID), data)
		}

		if needsCalendarUpdate(old, event) {
			logger.Debug("calendar upsert: UPDATED",
				"id", event.ID,
				"title", event.Title,
				"old_start", old.Start.Format(time.RFC3339),
				"new_start", event.Start.Format(time.RFC3339))
			data, err := json.Marshal(event)
			if err != nil {
				return err
			}
			result.Action = "updated"
			result.Verb = "updated"
			return b.Put([]byte(event.ID), data)
		}

		logger.Debug("calendar upsert: UNCHANGED (skipping)",
			"id", event.ID,
			"title", event.Title,
			"start", event.Start.Format(time.RFC3339),
			"reason", getUnchangedReason(old, event))
		result.Action = "unchanged"
		return nil
	})

	return result, err
}

func getUnchangedReason(old, new CalendarEvent) string {
	// This helps debug why we think it's unchanged
	return fmt.Sprintf("title_match=%v start_match=%v end_match=%v loc_match=%v status_match=%v updated_newer=%v",
		old.Title == new.Title,
		old.Start.Equal(new.Start),
		old.End.Equal(new.End),
		old.Location == new.Location,
		old.Status == new.Status,
		new.UpdatedAt.After(old.UpdatedAt))
}

func needsCalendarUpdate(old, new CalendarEvent) bool {
	if old.Title != new.Title {
		return true
	}
	if !old.Start.Equal(new.Start) {
		return true
	}
	if !old.End.Equal(new.End) {
		return true
	}
	if old.Location != new.Location {
		return true
	}
	if old.Status != new.Status {
		return true
	}
	if new.UpdatedAt.After(old.UpdatedAt) {
		return true
	}
	return false
}

// GetTodayEvents returns events for today
func (s *CalendarSyncer) GetTodayEvents() ([]CalendarEvent, error) {
	var events []CalendarEvent
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.AddDate(0, 0, 1)

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(calendarBucket))
		return b.ForEach(func(k, v []byte) error {
			var event CalendarEvent
			if err := json.Unmarshal(v, &event); err != nil {
				return err
			}
			// Include if event overlaps with today
			if event.Start.Before(endOfDay) && event.End.After(startOfDay) {
				events = append(events, event)
			}
			return nil
		})
	})

	return events, err
}

// GetNextEvent returns the next upcoming event
func (s *CalendarSyncer) GetNextEvent() (*CalendarEvent, error) {
	var next *CalendarEvent
	now := time.Now()

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(calendarBucket))
		return b.ForEach(func(k, v []byte) error {
			var event CalendarEvent
			if err := json.Unmarshal(v, &event); err != nil {
				return err
			}
			// Only future events
			if event.Start.After(now) && event.Status != "cancelled" {
				if next == nil || event.Start.Before(next.Start) {
					next = &event
				}
			}
			return nil
		})
	})

	return next, err
}

// GeneratePlanMyDay creates markdown for today's calendar
func (s *CalendarSyncer) GeneratePlanMyDay() (string, error) {
	events, err := s.GetTodayEvents()
	if err != nil {
		return "", err
	}

	if len(events) == 0 {
		return "## Calendar\n\nNo events today.\n", nil
	}

	var b strings.Builder
	b.WriteString("## Calendar\n\n")

	for _, event := range events {
		timeStr := event.Start.Format("15:04")
		b.WriteString(fmt.Sprintf("### %s [[%s]]\n", timeStr, event.Title))

		if len(event.Attendees) > 0 {
			b.WriteString(fmt.Sprintf("- attendees: %s\n", strings.Join(event.Attendees, ", ")))
		}
		if event.MeetLink != "" {
			b.WriteString(fmt.Sprintf("- link: %s\n", event.MeetLink))
		}
		if event.Location != "" {
			b.WriteString(fmt.Sprintf("- location: %s\n", event.Location))
		}

		b.WriteString("\n**Notes:**\n- \n\n")
	}

	return b.String(), nil
}

// StartPeriodicSync runs sync every interval and calls onChange with new/updated events
func (s *CalendarSyncer) StartPeriodicSync(ctx context.Context, interval time.Duration, onChange func([]CalendarEvent)) {
	ticker := time.NewTicker(interval)

	go func() {
		defer ticker.Stop()

		// Initial sync
		s.doSync(onChange)

		for {
			select {
			case <-ctx.Done():
				logger.Info("Calendar sync stopped")
				return
			case <-ticker.C:
				s.doSync(onChange)
			}
		}
	}()
}

func (s *CalendarSyncer) doSync(onChange func([]CalendarEvent)) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := s.Sync(ctx)
	if err != nil {
		logger.Error("Calendar sync failed", "error", err)
		return
	}

	logger.Info("Calendar sync complete",
		"created", len(result.Created),
		"updated", len(result.Updated),
		"cancelled", len(result.Cancelled),
		"unchanged", result.Unchanged,
		"errors", len(result.Errors))

	// Notify about changes
	var changes []CalendarEvent
	changes = append(changes, result.Created...)
	changes = append(changes, result.Updated...)
	changes = append(changes, result.Cancelled...)

	if len(changes) > 0 {
		onChange(changes)
	}
}

// normalizeCalendarName converts calendar ID/name to a choice label
func normalizeCalendarName(calID, calName string) string {
	// Primary calendar
	if calID == "primary" || strings.Contains(calID, "@gmail.com") || strings.Contains(calID, "@googlemail.com") {
		return "Primary"
	}
	// Work calendars (common patterns)
	if strings.Contains(strings.ToLower(calID), "work") || strings.Contains(strings.ToLower(calName), "work") {
		return "Work"
	}
	// Personal
	if strings.Contains(strings.ToLower(calName), "personal") {
		return "Personal"
	}
	// Default to Primary for user's main calendar (their email)
	if strings.Contains(calID, "@") && !strings.Contains(calID, "group.calendar.google.com") && !strings.Contains(calID, "import.calendar.google.com") {
		return "Primary"
	}
	// For shared/imported calendars, use the name
	return calName
}

// runCalendarTest fetches events from Google and prints detailed debug info
func runCalendarTest() {
	config := loadConfig()

	if len(config.GoogleCalendars) == 0 {
		fmt.Println("No calendars configured. Run: tm calendars enable <id>")
		return
	}

	tokens, err := loadGoogleTokens()
	if err != nil {
		fmt.Printf("Error loading tokens: %v\n", err)
		fmt.Println("Run: tm auth google")
		return
	}

	ctx := context.Background()
	oauthConfig := getGoogleOAuthConfig()
	token := &oauth2.Token{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    tokens.TokenType,
		Expiry:       tokens.Expiry,
	}
	tokenSource := oauthConfig.TokenSource(ctx, token)

	srv, err := calendar.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		fmt.Printf("Error creating calendar service: %v\n", err)
		return
	}

	// Date range
	now := time.Now()
	timeMin := now.AddDate(0, 0, -7)
	timeMax := now.AddDate(0, 0, 84)

	fmt.Println("=== CALENDAR TEST ===")
	fmt.Printf("Date range: %s to %s\n", timeMin.Format("2006-01-02"), timeMax.Format("2006-01-02"))
	fmt.Printf("Calendars: %v\n\n", config.GoogleCalendars)

	for _, calendarID := range config.GoogleCalendars {
		fmt.Printf("--- Calendar: %s ---\n", calendarID)

		events, err := srv.Events.List(calendarID).
			Context(ctx).
			TimeMin(timeMin.Format(time.RFC3339)).
			TimeMax(timeMax.Format(time.RFC3339)).
			SingleEvents(true).
			OrderBy("startTime").
			MaxResults(250).
			Do()

		if err != nil {
			fmt.Printf("Error fetching events: %v\n\n", err)
			continue
		}

		fmt.Printf("Raw events from Google: %d\n\n", len(events.Items))

		for i, item := range events.Items {
			fmt.Printf("[%d] RAW FROM GOOGLE:\n", i+1)
			fmt.Printf("    Id:          %s\n", item.Id)
			fmt.Printf("    Summary:     %s\n", item.Summary)
			fmt.Printf("    Status:      %s\n", item.Status)
			fmt.Printf("    Start.DateTime: %s\n", item.Start.DateTime)
			fmt.Printf("    Start.Date:     %s\n", item.Start.Date)
			fmt.Printf("    End.DateTime:   %s\n", item.End.DateTime)
			fmt.Printf("    End.Date:       %s\n", item.End.Date)
			fmt.Printf("    RecurringEventId: %s\n", item.RecurringEventId)
			if len(item.Attendees) > 0 {
				fmt.Printf("    Attendees:   %d\n", len(item.Attendees))
			}
			fmt.Println()

			// Parse it
			var startTime, endTime time.Time
			allDay := false
			if item.Start.DateTime != "" {
				startTime, _ = time.Parse(time.RFC3339, item.Start.DateTime)
			} else if item.Start.Date != "" {
				startTime, _ = time.Parse("2006-01-02", item.Start.Date)
				allDay = true
			}
			if item.End.DateTime != "" {
				endTime, _ = time.Parse(time.RFC3339, item.End.DateTime)
			} else if item.End.Date != "" {
				endTime, _ = time.Parse("2006-01-02", item.End.Date)
			}

			fmt.Printf("    PARSED:\n")
			fmt.Printf("    ID (for Thymer): gcal_%s\n", item.Id)
			fmt.Printf("    Start:       %s (AllDay: %v)\n", startTime.Format(time.RFC3339), allDay)
			fmt.Printf("    End:         %s\n", endTime.Format(time.RFC3339))
			fmt.Printf("    Start ms:    %d\n", startTime.UnixMilli())
			fmt.Printf("    Start sec:   %d\n", startTime.Unix())

			// Show what would be sent to Thymer (first 3 only)
			if i < 3 {
				event := CalendarEvent{
					ID:          fmt.Sprintf("gcal_%s", item.Id),
					CalendarID:  calendarID,
					Title:       item.Summary,
					Start:       startTime,
					End:         endTime,
					AllDay:      allDay,
					Status:      item.Status,
					Verb:        "created",
				}
				fmt.Println("\n    MARKDOWN OUTPUT:")
				for _, line := range strings.Split(event.ToMarkdown(), "\n") {
					fmt.Printf("    | %s\n", line)
				}
			}
			fmt.Println()
		}
	}
}

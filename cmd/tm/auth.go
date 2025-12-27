package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

const (
	// OAuth callback port - different from server port
	OAuthCallbackPort = "19502"
	OAuthCallbackURL  = "http://localhost:19502/callback"

	// Google OAuth Client ID and Secret
	// These are for a "Desktop app" OAuth client in Google Cloud Console
	// Users can replace with their own if needed
	GoogleClientID     = "YOUR_CLIENT_ID.apps.googleusercontent.com"
	GoogleClientSecret = "YOUR_CLIENT_SECRET"
)

// GoogleTokens holds OAuth tokens for Google APIs
type GoogleTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
	Email        string    `json:"email,omitempty"`
}

// getGoogleOAuthConfig returns the OAuth2 config for Google Calendar
func getGoogleOAuthConfig() *oauth2.Config {
	cfg := loadConfig()
	clientID := cfg.GoogleClientID
	clientSecret := cfg.GoogleClientSecret

	// Fall back to hardcoded defaults if not in config
	if clientID == "" {
		clientID = GoogleClientID
	}
	if clientSecret == "" {
		clientSecret = GoogleClientSecret
	}

	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{calendar.CalendarReadonlyScope},
		Endpoint:     google.Endpoint,
		RedirectURL:  OAuthCallbackURL,
	}
}

// runGoogleAuth runs the OAuth browser flow for Google Calendar
func runGoogleAuth() {
	fmt.Println("🔐 Google Calendar Authentication")
	fmt.Println()

	// Check if already authenticated
	tokens, err := loadGoogleTokens()
	if err == nil && tokens.RefreshToken != "" {
		fmt.Printf("Already authenticated as: %s\n", tokens.Email)
		fmt.Println()
		fmt.Println("Run 'tm auth google --force' to re-authenticate")
		fmt.Println("Run 'tm calendars' to see available calendars")
		return
	}

	// Generate state for CSRF protection
	state, err := generateState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating state: %v\n", err)
		os.Exit(1)
	}

	config := getGoogleOAuthConfig()

	// Check if client ID is configured
	if config.ClientID == "YOUR_CLIENT_ID.apps.googleusercontent.com" {
		fmt.Println("⚠️  Google OAuth not configured!")
		fmt.Println()
		fmt.Println("To set up Google Calendar sync:")
		fmt.Println()
		fmt.Println("1. Go to https://console.cloud.google.com/apis/credentials")
		fmt.Println("2. Create a new OAuth 2.0 Client ID (Desktop app)")
		fmt.Println("3. Enable the Google Calendar API")
		fmt.Println("4. Add your client ID and secret to ~/.config/tm/config:")
		fmt.Println()
		fmt.Println("   google_client_id=YOUR_CLIENT_ID.apps.googleusercontent.com")
		fmt.Println("   google_client_secret=YOUR_CLIENT_SECRET")
		fmt.Println()
		fmt.Println("5. Run 'tm auth google' again")
		os.Exit(1)
	}

	// Create channel to receive the auth code
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Start local server to receive callback
	server := &http.Server{Addr: ":" + OAuthCallbackPort}

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// Verify state
		if r.URL.Query().Get("state") != state {
			errChan <- fmt.Errorf("invalid state parameter")
			http.Error(w, "Invalid state", http.StatusBadRequest)
			return
		}

		// Check for error
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errChan <- fmt.Errorf("auth error: %s", errMsg)
			http.Error(w, "Authentication failed: "+errMsg, http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errChan <- fmt.Errorf("no code in callback")
			http.Error(w, "No authorization code", http.StatusBadRequest)
			return
		}

		// Success page
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Authentication Successful</title></head>
<body style="font-family: system-ui; max-width: 600px; margin: 50px auto; text-align: center;">
<h1>✅ Authentication Successful</h1>
<p>You can close this window and return to the terminal.</p>
</body>
</html>`)

		codeChan <- code
	})

	// Start server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Generate auth URL
	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	fmt.Println("Opening browser for Google sign-in...")
	fmt.Printf("(listening on localhost:%s for callback)\n", OAuthCallbackPort)
	fmt.Println()

	// Open browser
	if err := openBrowser(authURL); err != nil {
		fmt.Println("Could not open browser automatically.")
		fmt.Println("Please open this URL manually:")
		fmt.Println()
		fmt.Println(authURL)
		fmt.Println()
	}

	// Wait for callback or timeout
	select {
	case code := <-codeChan:
		// Exchange code for tokens
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		token, err := config.Exchange(ctx, code)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error exchanging code: %v\n", err)
			os.Exit(1)
		}

		// Get user email
		email := getUserEmail(ctx, config, token)

		// Save tokens
		tokens := GoogleTokens{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			TokenType:    token.TokenType,
			Expiry:       token.Expiry,
			Email:        email,
		}

		if err := saveGoogleTokens(tokens); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving tokens: %v\n", err)
			os.Exit(1)
		}

		fmt.Println()
		fmt.Printf("✅ Authenticated as %s\n", email)
		fmt.Println("✅ Token saved to ~/.config/tm/google.json")
		fmt.Println()

		// List calendars
		listCalendarsAfterAuth(ctx, config, token)

	case err := <-errChan:
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)

	case <-time.After(2 * time.Minute):
		fmt.Fprintf(os.Stderr, "Timeout waiting for authentication\n")
		os.Exit(1)
	}

	// Shutdown server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}

// listCalendarsAfterAuth lists calendars after successful authentication
func listCalendarsAfterAuth(ctx context.Context, config *oauth2.Config, token *oauth2.Token) {
	srv, err := calendar.NewService(ctx, option.WithTokenSource(config.TokenSource(ctx, token)))
	if err != nil {
		return
	}

	list, err := srv.CalendarList.List().Context(ctx).Do()
	if err != nil {
		return
	}

	fmt.Printf("Found %d calendars:\n", len(list.Items))
	for _, cal := range list.Items {
		marker := "  "
		if cal.Primary {
			marker = "✓ "
		}
		name := cal.Summary
		if cal.SummaryOverride != "" {
			name = cal.SummaryOverride
		}
		fmt.Printf("  %s%s (%s)\n", marker, cal.Id, name)
	}
	fmt.Println()
	fmt.Println("Add calendars to sync in ~/.config/tm/config:")
	fmt.Println("  google_calendars=primary,work@company.com")
	fmt.Println()
	fmt.Println("Or run 'tm calendars enable <id>' to add one")
}

// runListCalendars lists available Google calendars
func runListCalendars() {
	tokens, err := loadGoogleTokens()
	if err != nil {
		fmt.Println("Not authenticated with Google.")
		fmt.Println("Run 'tm auth google' first.")
		os.Exit(1)
	}

	config := getGoogleOAuthConfig()
	ctx := context.Background()

	token := &oauth2.Token{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    tokens.TokenType,
		Expiry:       tokens.Expiry,
	}

	srv, err := calendar.NewService(ctx, option.WithTokenSource(config.TokenSource(ctx, token)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating calendar service: %v\n", err)
		os.Exit(1)
	}

	list, err := srv.CalendarList.List().Context(ctx).Do()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing calendars: %v\n", err)
		os.Exit(1)
	}

	// Load enabled calendars from config
	cfg := loadConfig()
	enabled := make(map[string]bool)
	for _, id := range cfg.GoogleCalendars {
		enabled[id] = true
	}

	fmt.Printf("Google Calendars for %s:\n", tokens.Email)
	fmt.Println()
	for _, cal := range list.Items {
		marker := "  "
		if enabled[cal.Id] || (cal.Primary && enabled["primary"]) {
			marker = "✓ "
		}
		name := cal.Summary
		if cal.SummaryOverride != "" {
			name = cal.SummaryOverride
		}
		if cal.Primary {
			fmt.Printf("  %sprimary (%s)\n", marker, name)
		} else {
			fmt.Printf("  %s%s (%s)\n", marker, cal.Id, name)
		}
	}
	fmt.Println()
	if len(cfg.GoogleCalendars) == 0 {
		fmt.Println("No calendars enabled for sync.")
		fmt.Println("Run 'tm calendars enable <id>' to add one")
	}
}

// runCalendarsEnable enables a calendar for syncing
func runCalendarsEnable(calendarID string) {
	cfg := loadConfig()

	// Check if already enabled
	for _, id := range cfg.GoogleCalendars {
		if id == calendarID {
			fmt.Printf("Calendar '%s' is already enabled\n", calendarID)
			return
		}
	}

	// Add to config file
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".config", "tm", "config")

	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}

	content := string(data)
	newCalendars := append(cfg.GoogleCalendars, calendarID)
	calendarLine := "google_calendars=" + joinCalendars(newCalendars)

	// Update or add the line
	lines := strings.Split(content, "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, "google_calendars=") {
			lines[i] = calendarLine
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, calendarLine)
	}

	err = os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Enabled calendar: %s\n", calendarID)
	fmt.Println("Restart 'tm serve' to start syncing")
}

// runCalendarsDisable disables a calendar from syncing
func runCalendarsDisable(calendarID string) {
	cfg := loadConfig()

	// Check if enabled
	found := false
	var newCalendars []string
	for _, id := range cfg.GoogleCalendars {
		if id == calendarID {
			found = true
		} else {
			newCalendars = append(newCalendars, id)
		}
	}

	if !found {
		fmt.Printf("Calendar '%s' is not enabled\n", calendarID)
		return
	}

	// Update config file
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".config", "tm", "config")

	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}

	content := string(data)
	calendarLine := "google_calendars=" + joinCalendars(newCalendars)

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "google_calendars=") {
			if len(newCalendars) == 0 {
				// Remove the line
				lines = append(lines[:i], lines[i+1:]...)
			} else {
				lines[i] = calendarLine
			}
			break
		}
	}

	err = os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Disabled calendar: %s\n", calendarID)
	fmt.Println("Restart 'tm serve' to apply changes")
}

// Helper functions

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}

func getUserEmail(ctx context.Context, config *oauth2.Config, token *oauth2.Token) string {
	srv, err := calendar.NewService(ctx, option.WithTokenSource(config.TokenSource(ctx, token)))
	if err != nil {
		return ""
	}

	// Get primary calendar to find email
	cal, err := srv.CalendarList.Get("primary").Context(ctx).Do()
	if err != nil {
		return ""
	}

	return cal.Id
}

func loadGoogleTokens() (*GoogleTokens, error) {
	home, _ := os.UserHomeDir()
	tokenPath := filepath.Join(home, ".config", "tm", "google.json")

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, err
	}

	var tokens GoogleTokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}

	return &tokens, nil
}

func saveGoogleTokens(tokens GoogleTokens) error {
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".config", "tm")
	os.MkdirAll(configDir, 0700)

	tokenPath := filepath.Join(configDir, "google.json")

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(tokenPath, data, 0600)
}

func joinCalendars(calendars []string) string {
	return strings.Join(calendars, ",")
}

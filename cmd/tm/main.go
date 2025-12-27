// tm - Thymer queue CLI
//
// Usage:
//   cat README.md | tm              Push markdown to queue (action: append)
//   echo "Meeting notes" | tm       Push to queue
//   tm lifelog Had coffee           Push lifelog entry
//   tm --collection "Tasks" < x.md  Push with collection target
//   tm serve                        Run local server (same API as Cloudflare Worker)
//
// Config: Set THYMER_URL and THYMER_TOKEN environment variables
//         or create ~/.config/tm/config with url= and token= lines
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

var logger *slog.Logger

const (
	LocalServerPort = "19501"
	LocalServerURL  = "http://localhost:19501"
)

type Config struct {
	URL                string
	Token              string
	GitHubToken        string
	GitHubRepos        []string
	ReadwiseToken      string
	GoogleClientID     string
	GoogleClientSecret string
	GoogleCalendars    []string
}

type QueueItem struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	Action     string `json:"action,omitempty"`
	Collection string `json:"collection,omitempty"`
	Title      string `json:"title,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

func main() {
	args := os.Args[1:]

	// Handle special commands first (before config check)
	if len(args) > 0 {
		switch args[0] {
		case "serve":
			runServer()
			return
		case "auth":
			if len(args) > 1 && args[1] == "google" {
				runGoogleAuth()
			} else {
				fmt.Println("Usage: tm auth google")
			}
			return
		case "calendar":
			if len(args) > 1 && args[1] == "test" {
				runCalendarTest()
				return
			}
			fmt.Println("Usage: tm calendar test")
			return
		case "calendars":
			if len(args) > 1 {
				switch args[1] {
				case "enable":
					if len(args) > 2 {
						runCalendarsEnable(args[2])
					} else {
						fmt.Println("Usage: tm calendars enable <calendar-id>")
					}
				case "disable":
					if len(args) > 2 {
						runCalendarsDisable(args[2])
					} else {
						fmt.Println("Usage: tm calendars disable <calendar-id>")
					}
				default:
					runListCalendars()
				}
			} else {
				runListCalendars()
			}
			return
		case "sync":
			// Trigger sync via HTTP endpoint (no cache clear)
			if len(args) > 1 {
				switch args[1] {
				case "github":
					triggerHTTPSync("github", false)
				case "calendar":
					triggerHTTPSync("calendar", false)
				case "readwise":
					triggerHTTPSync("readwise", false)
				default:
					fmt.Println("Usage: tm sync [github|calendar|readwise]")
				}
			} else {
				fmt.Println("Usage: tm sync [github|calendar|readwise]")
			}
			return
		case "resync":
			// Trigger sync via HTTP endpoint WITH cache clear
			if len(args) > 1 {
				switch args[1] {
				case "github":
					triggerHTTPSync("github", true)
				case "calendar":
					triggerHTTPSync("calendar", true)
				case "readwise":
					triggerHTTPSync("readwise", true)
				default:
					fmt.Println("Usage: tm resync [github|calendar|readwise]")
				}
			} else {
				// Resync all
				triggerHTTPSync("github", true)
				triggerHTTPSync("calendar", true)
				triggerHTTPSync("readwise", true)
			}
			return
		case "readwise-sync":
			triggerReadwiseSync()
			return
		case "--help", "-h", "help":
			printUsage()
			return
		}
	}

	config := loadConfig()

	if config.URL == "" || config.Token == "" {
		fmt.Fprintln(os.Stderr, "Error: THYMER_URL and THYMER_TOKEN required")
		fmt.Fprintln(os.Stderr, "Set environment variables or create ~/.config/tm/config")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "For local development, run: tm serve")
		os.Exit(1)
	}

	// Parse arguments
	req := QueueItem{Action: "append"}

	// Parse flags
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--collection", "-c":
			if i+1 < len(args) {
				req.Collection = args[i+1]
				i += 2
				continue
			}
		case "--title", "-t":
			if i+1 < len(args) {
				req.Title = args[i+1]
				i += 2
				continue
			}
		case "--action", "-a":
			if i+1 < len(args) {
				req.Action = args[i+1]
				i += 2
				continue
			}
		case "lifelog":
			req.Action = "lifelog"
			// Rest of args become the content
			if i+1 < len(args) {
				req.Content = strings.Join(args[i+1:], " ")
			}
			i = len(args)
			continue
		case "create":
			req.Action = "create"
			i++
			continue
		case "--help", "-h":
			printUsage()
			return
		}
		i++
	}

	// If no content from args, read from stdin
	if req.Content == "" {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
				os.Exit(1)
			}
			req.Content = string(data)
		}
	}

	if req.Content == "" {
		printUsage()
		os.Exit(1)
	}

	// Add timestamp from CLI (includes timezone)
	req.CreatedAt = time.Now().Format(time.RFC3339)

	// Send to queue
	if err := sendToQueue(config, req); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Queued %d bytes (%s)\n", len(req.Content), req.Action)
}

func sendToQueue(config Config, req QueueItem) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequest("POST", config.URL+"/queue", bytes.NewReader(body))
	if err != nil {
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+config.Token)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ============================================================================
// Server Mode - implements same API as Cloudflare Worker
// ============================================================================

type Server struct {
	queue      map[string]QueueItem
	mu         sync.RWMutex
	token      string
	ghSyncer   *GitHubSyncer
	rwSyncer   *ReadwiseSyncer
	calSyncer  *CalendarSyncer
}

func resyncRepo(repo string) {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".config", "tm", "github.db")

	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	var deleted int
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("github_issues"))
		if b == nil {
			return nil
		}

		var keysToDelete [][]byte
		b.ForEach(func(k, v []byte) error {
			key := string(k)
			// If repo specified, only delete matching keys
			// Key format: github_owner_repo_123
			if repo == "" {
				keysToDelete = append(keysToDelete, k)
			} else {
				repoSlug := strings.ReplaceAll(repo, "/", "_")
				if strings.Contains(key, repoSlug) {
					keysToDelete = append(keysToDelete, k)
				}
			}
			return nil
		})

		for _, k := range keysToDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
			deleted++
		}
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if repo == "" {
		fmt.Printf("✓ Cleared all %d GitHub issues from cache\n", deleted)
	} else {
		fmt.Printf("✓ Cleared %d issues for %s from cache\n", deleted, repo)
	}
	fmt.Println("  Restart 'tm serve' to resync")
}

func resyncReadwise() {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".config", "tm", "readwise.db")

	// Check if file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Println("✓ No Readwise cache to clear")
		return
	}

	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	var deleted int
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("documents"))
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
			deleted++
		}

		// Also clear sync metadata
		if meta := tx.Bucket([]byte("sync_meta")); meta != nil {
			meta.Delete([]byte("last_sync"))
		}

		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Cleared %d Readwise documents from cache\n", deleted)
	fmt.Println("  Restart 'tm serve' to resync")
}

func resyncCalendar() {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".config", "tm", "calendar.db")

	// Check if file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Println("✓ No Calendar cache to clear")
		return
	}

	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	var deleted int
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(calendarBucket))
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
			deleted++
		}

		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Cleared %d Calendar events from cache\n", deleted)
	fmt.Println("  Restart 'tm serve' to resync")
}

func triggerReadwiseSync() {
	config := loadConfig()

	url := config.URL
	if url == "" {
		url = LocalServerURL
	}
	token := config.Token
	if token == "" {
		token = "local-dev-token"
	}

	req, err := http.NewRequest("POST", url+"/readwise-sync?token="+token, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v (is 'tm serve' running?)\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(body))
		os.Exit(1)
	}

	fmt.Println("✓ Readwise sync triggered")
}

func triggerHTTPSync(syncType string, resync bool) {
	config := loadConfig()

	url := config.URL
	if url == "" {
		url = LocalServerURL
	}
	token := config.Token
	if token == "" {
		token = "local-dev-token"
	}

	endpoint := fmt.Sprintf("%s/sync/%s?token=%s", url, syncType, token)
	if resync {
		endpoint += "&resync=true"
	}

	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v (is 'tm serve' running?)\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(body))
		os.Exit(1)
	}

	action := "sync"
	if resync {
		action = "resync"
	}
	fmt.Printf("✓ %s %s triggered\n", strings.Title(syncType), action)
}

func runServer() {
	// Check for verbose flag
	verbose := false
	for _, arg := range os.Args[2:] {
		if arg == "-v" || arg == "--verbose" {
			verbose = true
			break
		}
	}

	// Initialize logger
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))

	config := loadConfig()

	token := config.Token
	if token == "" {
		token = "local-dev-token"
		logger.Warn("no THYMER_TOKEN set, using default", "token", token)
	}

	srv := &Server{
		queue: make(map[string]QueueItem),
		token: token,
	}

	// Start GitHub sync if configured
	if config.GitHubToken != "" && len(config.GitHubRepos) > 0 {
		home, _ := os.UserHomeDir()
		dataDir := filepath.Join(home, ".config", "tm")
		os.MkdirAll(dataDir, 0755)

		syncer, err := NewGitHubSyncer(config.GitHubToken, config.GitHubRepos, dataDir)
		if err != nil {
			logger.Warn("GitHub sync disabled", "error", err)
		} else {
			srv.ghSyncer = syncer
			ctx := context.Background()
			syncer.StartPeriodicSync(ctx, 1*time.Minute, func(issues []GitHubIssue) {
				srv.queueGitHubChanges(issues)
			})
			logger.Info("GitHub sync enabled", "repos", strings.Join(config.GitHubRepos, ", "))
		}
	}

	// Start Readwise sync if configured
	if config.ReadwiseToken != "" {
		home, _ := os.UserHomeDir()
		dataDir := filepath.Join(home, ".config", "tm")
		os.MkdirAll(dataDir, 0755)

		syncer, err := NewReadwiseSyncer(config.ReadwiseToken, dataDir)
		if err != nil {
			logger.Warn("Readwise sync disabled", "error", err)
		} else {
			srv.rwSyncer = syncer
			go srv.startReadwiseSync(1 * time.Hour)
			logger.Info("Readwise sync enabled", "interval", "1h")
		}
	}

	// Start Google Calendar sync if configured
	if len(config.GoogleCalendars) > 0 {
		tokens, err := loadGoogleTokens()
		if err != nil {
			logger.Warn("Calendar sync disabled", "error", "not authenticated - run 'tm auth google'")
		} else {
			home, _ := os.UserHomeDir()
			dataDir := filepath.Join(home, ".config", "tm")

			calTokens := &CalendarTokens{
				AccessToken:  tokens.AccessToken,
				RefreshToken: tokens.RefreshToken,
				TokenType:    tokens.TokenType,
				Expiry:       tokens.Expiry,
			}

			syncer, err := NewCalendarSyncer(calTokens, config.GoogleCalendars, dataDir)
			if err != nil {
				logger.Warn("Calendar sync disabled", "error", err)
			} else {
				srv.calSyncer = syncer
				ctx := context.Background()
				syncer.StartPeriodicSync(ctx, 5*time.Minute, func(events []CalendarEvent) {
					srv.queueCalendarChanges(events)
				})
				logger.Info("Calendar sync enabled", "calendars", strings.Join(config.GoogleCalendars, ", "), "interval", "5m")
			}
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/readwise-sync", srv.handleReadwiseSync)
	mux.HandleFunc("/sync/github", srv.handleGitHubSync)
	mux.HandleFunc("/sync/calendar", srv.handleCalendarSync)
	mux.HandleFunc("/sync/readwise", srv.handleReadwiseSync)
	mux.HandleFunc("/queue", srv.handleQueue)
	mux.HandleFunc("/stream", srv.handleStream)
	mux.HandleFunc("/pending", srv.handlePending)
	mux.HandleFunc("/peek", srv.handlePeek)

	logger.Info("server starting", "port", LocalServerPort, "token", token)

	if err := http.ListenAndServe(":"+LocalServerPort, srv.corsMiddleware(mux)); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func (s *Server) queueGitHubChanges(issues []GitHubIssue) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, issue := range issues {
		item := QueueItem{
			ID:        fmt.Sprintf("gh-%d", time.Now().UnixNano()),
			Action:    "append",
			Title:     issue.Title,
			Content:   issue.ToMarkdown(),
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		s.queue[item.ID] = item
		logger.Debug("queued GitHub issue", "repo", issue.Repo, "number", issue.Number, "state", issue.State)
	}
}

func (s *Server) queueCalendarChanges(events []CalendarEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, event := range events {
		item := QueueItem{
			ID:        fmt.Sprintf("cal-%d", time.Now().UnixNano()),
			Action:    "append",
			Title:     event.Title,
			Content:   event.ToMarkdown(),
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		s.queue[item.ID] = item
		logger.Debug("queued calendar event", "title", event.Title, "start", event.Start.Format("2006-01-02 15:04"), "verb", event.Verb)
	}
}

func (s *Server) startReadwiseSync(interval time.Duration) {
	// Initial sync after short delay (let server start)
	time.Sleep(5 * time.Second)
	s.doReadwiseSync()

	// Periodic sync
	ticker := time.NewTicker(interval)
	for range ticker.C {
		s.doReadwiseSync()
	}
}

func (s *Server) handleReadwiseSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	if !s.checkAuth(r) {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if s.rwSyncer == nil {
		http.Error(w, `{"error":"Readwise sync not configured"}`, http.StatusBadRequest)
		return
	}

	go s.doReadwiseSync()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "sync started"})
}

func (s *Server) doReadwiseSync() {
	if s.rwSyncer == nil {
		return
	}

	docs, err := s.rwSyncer.Sync()
	if err != nil {
		logger.Error("Readwise sync failed", "error", err)
		return
	}

	if len(docs) == 0 {
		logger.Debug("Readwise sync complete", "changes", 0)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, doc := range docs {
		item := QueueItem{
			ID:        fmt.Sprintf("rw-%d", time.Now().UnixNano()),
			Action:    "append",
			Title:     doc.Document.Title,
			Content:   doc.ToMarkdown(),
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		s.queue[item.ID] = item
		status := "updated"
		if doc.IsNew {
			status = "new"
		}
		logger.Debug("queued Readwise", "title", doc.Document.Title, "status", status, "highlights", len(doc.Highlights))
	}
	logger.Info("Readwise sync complete", "documents", len(docs))
}

func (s *Server) handleGitHubSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	if !s.checkAuth(r) {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if s.ghSyncer == nil {
		http.Error(w, `{"error":"GitHub sync not configured"}`, http.StatusBadRequest)
		return
	}

	// Check for resync flag to clear cache first
	if r.URL.Query().Get("resync") == "true" {
		if err := s.ghSyncer.ClearCache(); err != nil {
			logger.Error("failed to clear GitHub cache", "error", err)
		} else {
			logger.Info("GitHub cache cleared for resync")
		}
	}

	go s.ghSyncer.doSync(func(issues []GitHubIssue) {
		s.queueGitHubChanges(issues)
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "sync started"})
}

func (s *Server) handleCalendarSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	if !s.checkAuth(r) {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if s.calSyncer == nil {
		http.Error(w, `{"error":"Calendar sync not configured"}`, http.StatusBadRequest)
		return
	}

	// Check for resync flag to clear cache first
	if r.URL.Query().Get("resync") == "true" {
		if err := s.calSyncer.ClearCache(); err != nil {
			logger.Error("failed to clear calendar cache", "error", err)
		} else {
			logger.Info("Calendar cache cleared for resync")
		}
	}

	go s.calSyncer.doSync(func(events []CalendarEvent) {
		s.queueCalendarChanges(events)
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "sync started"})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		// Allow requests from public websites to localhost (Private Network Access)
		w.Header().Set("Access-Control-Allow-Private-Network", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) checkAuth(r *http.Request) bool {
	// Auth via header or query param
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	return token == s.token
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	if !s.checkAuth(r) {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req QueueItem
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if req.Content == "" {
		http.Error(w, `{"error":"content required"}`, http.StatusBadRequest)
		return
	}

	// Generate ID with timestamp for ordering
	req.ID = fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixNano()%1000)
	req.CreatedAt = time.Now().Format(time.RFC3339)

	s.mu.Lock()
	s.queue[req.ID] = req
	s.mu.Unlock()

	logger.Debug("queued", "action", req.Action, "bytes", len(req.Content))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": req.ID})
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Send connected event
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	logger.Info("SSE client connected")

	// Check queue every 2 seconds for 25 seconds
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(25 * time.Second)

	for {
		select {
		case <-ticker.C:
			item := s.popOldest()
			if item != nil {
				data, _ := json.Marshal(item)
				fmt.Fprintf(w, "data: %s\n\n", data)
				logger.Debug("sent", "action", item.Action, "bytes", len(item.Content))
			} else {
				fmt.Fprintf(w, ": heartbeat\n\n")
			}
			flusher.Flush()

		case <-timeout:
			logger.Debug("SSE timeout, client will reconnect")
			return

		case <-r.Context().Done():
			logger.Info("SSE client disconnected")
			return
		}
	}
}

func (s *Server) handlePending(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	item := s.popOldest()
	if item == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	logger.Debug("sent (poll)", "action", item.Action, "bytes", len(item.Content))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(item)
}

func (s *Server) handlePeek(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	s.mu.RLock()
	items := make([]QueueItem, 0, len(s.queue))
	for _, item := range s.queue {
		items = append(items, item)
	}
	s.mu.RUnlock()

	// Sort by ID (timestamp-based)
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count": len(items),
		"items": items,
	})
}

func (s *Server) popOldest() *QueueItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.queue) == 0 {
		return nil
	}

	// Find oldest by ID
	var oldestID string
	for id := range s.queue {
		if oldestID == "" || id < oldestID {
			oldestID = id
		}
	}

	item := s.queue[oldestID]
	delete(s.queue, oldestID)
	return &item
}

// ============================================================================
// Config
// ============================================================================

func loadConfig() Config {
	config := Config{
		URL:           os.Getenv("THYMER_URL"),
		Token:         os.Getenv("THYMER_TOKEN"),
		GitHubToken:   os.Getenv("GITHUB_TOKEN"),
		ReadwiseToken: os.Getenv("READWISE_TOKEN"),
	}

	if repos := os.Getenv("GITHUB_REPOS"); repos != "" {
		config.GitHubRepos = parseRepoList(repos)
	}

	// Try config file
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".config", "tm", "config")
	data, err := os.ReadFile(configPath)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "url=") && config.URL == "" {
				config.URL = strings.TrimPrefix(line, "url=")
			}
			if strings.HasPrefix(line, "token=") && config.Token == "" {
				config.Token = strings.TrimPrefix(line, "token=")
			}
			if strings.HasPrefix(line, "github_token=") && config.GitHubToken == "" {
				config.GitHubToken = strings.TrimPrefix(line, "github_token=")
			}
			if strings.HasPrefix(line, "github_repos=") && len(config.GitHubRepos) == 0 {
				config.GitHubRepos = parseRepoList(strings.TrimPrefix(line, "github_repos="))
			}
			if strings.HasPrefix(line, "readwise_token=") && config.ReadwiseToken == "" {
				config.ReadwiseToken = strings.TrimPrefix(line, "readwise_token=")
			}
			if strings.HasPrefix(line, "google_client_id=") && config.GoogleClientID == "" {
				config.GoogleClientID = strings.TrimPrefix(line, "google_client_id=")
			}
			if strings.HasPrefix(line, "google_client_secret=") && config.GoogleClientSecret == "" {
				config.GoogleClientSecret = strings.TrimPrefix(line, "google_client_secret=")
			}
			if strings.HasPrefix(line, "google_calendars=") && len(config.GoogleCalendars) == 0 {
				config.GoogleCalendars = parseRepoList(strings.TrimPrefix(line, "google_calendars="))
			}
		}
	}

	return config
}

func parseRepoList(s string) []string {
	var repos []string
	for _, r := range strings.Split(s, ",") {
		r = strings.TrimSpace(r)
		if r != "" {
			repos = append(repos, r)
		}
	}
	return repos
}

func printUsage() {
	fmt.Println("tm - Thymer queue CLI")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  cat file.md | tm                    Push markdown to Thymer")
	fmt.Println("  echo 'note' | tm                    Push text to Thymer")
	fmt.Println("  tm lifelog Had coffee with Alex     Push lifelog entry")
	fmt.Println("  tm --collection 'Tasks' < todo.md   Push to specific collection")
	fmt.Println("  tm create --title 'New Note'        Create new record")
	fmt.Println("  tm serve                            Run local queue server")
	fmt.Println("  tm resync [repo|readwise|calendar]  Clear sync cache (resync on next serve)")
	fmt.Println("  tm readwise-sync                    Trigger Readwise sync now")
	fmt.Println()
	fmt.Println("Google Calendar:")
	fmt.Println("  tm auth google                      Authenticate with Google")
	fmt.Println("  tm calendars                        List available calendars")
	fmt.Println("  tm calendars enable <id>            Enable calendar for sync")
	fmt.Println("  tm calendars disable <id>           Disable calendar from sync")
	fmt.Println()
	fmt.Println("Actions:")
	fmt.Println("  append (default)  Append to daily page")
	fmt.Println("  lifelog           Add timestamped lifelog entry")
	fmt.Println("  create            Create new record in collection")
	fmt.Println()
	fmt.Println("Server mode:")
	fmt.Printf("  tm serve                            Start server on port %s\n", LocalServerPort)
	fmt.Println("  tm serve -v                         Verbose logging (debug level)")
	fmt.Println()
	fmt.Println("Config:")
	fmt.Println("  Set THYMER_URL and THYMER_TOKEN environment variables")
	fmt.Println("  Or create ~/.config/tm/config with:")
	fmt.Println("    url=https://thymer.lifelog.my")
	fmt.Println("    token=your-secret-token")
	fmt.Println()
	fmt.Println("  For Google Calendar:")
	fmt.Println("    google_client_id=YOUR_ID.apps.googleusercontent.com")
	fmt.Println("    google_client_secret=YOUR_SECRET")
	fmt.Println("    google_calendars=primary,work@company.com")
	fmt.Println()
	fmt.Println("  For local development:")
	fmt.Printf("    url=%s\n", LocalServerURL)
	fmt.Println("    token=local-dev-token")
}

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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	LocalServerPort = "19501"
	LocalServerURL  = "http://localhost:19501"
)

type Config struct {
	URL   string
	Token string
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
	queue map[string]QueueItem
	mu    sync.RWMutex
	token string
}

func runServer() {
	token := os.Getenv("THYMER_TOKEN")
	if token == "" {
		// Generate a simple token for local dev
		token = "local-dev-token"
		fmt.Printf("⚠️  No THYMER_TOKEN set, using: %s\n", token)
	}

	srv := &Server{
		queue: make(map[string]QueueItem),
		token: token,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/queue", srv.handleQueue)
	mux.HandleFunc("/stream", srv.handleStream)
	mux.HandleFunc("/pending", srv.handlePending)
	mux.HandleFunc("/peek", srv.handlePeek)

	fmt.Printf("🪄 Thymer queue server on http://localhost:%s\n", LocalServerPort)
	fmt.Printf("   Token: %s\n", token)
	fmt.Println()
	fmt.Println("   POST /queue   - Add to queue")
	fmt.Println("   GET  /stream  - SSE stream")
	fmt.Println("   GET  /pending - Poll (legacy)")
	fmt.Println("   GET  /peek    - View queue")
	fmt.Println()
	fmt.Println("   Ctrl+C to stop")

	if err := http.ListenAndServe(":"+LocalServerPort, srv.corsMiddleware(mux)); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
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

	fmt.Printf("📥 Queued: %s (%d bytes)\n", req.Action, len(req.Content))

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

	fmt.Println("📡 SSE client connected")

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
				fmt.Printf("📤 Sent: %s (%d bytes)\n", item.Action, len(item.Content))
			} else {
				fmt.Fprintf(w, ": heartbeat\n\n")
			}
			flusher.Flush()

		case <-timeout:
			fmt.Println("📡 SSE timeout, client will reconnect")
			return

		case <-r.Context().Done():
			fmt.Println("📡 SSE client disconnected")
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

	fmt.Printf("📤 Sent (poll): %s (%d bytes)\n", item.Action, len(item.Content))

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
		URL:   os.Getenv("THYMER_URL"),
		Token: os.Getenv("THYMER_TOKEN"),
	}

	// Try config file if env vars not set
	if config.URL == "" || config.Token == "" {
		home, _ := os.UserHomeDir()
		configPath := filepath.Join(home, ".config", "tm", "config")
		data, err := os.ReadFile(configPath)
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "url=") {
					config.URL = strings.TrimPrefix(line, "url=")
				}
				if strings.HasPrefix(line, "token=") {
					config.Token = strings.TrimPrefix(line, "token=")
				}
			}
		}
	}

	return config
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
	fmt.Println()
	fmt.Println("Actions:")
	fmt.Println("  append (default)  Append to daily page")
	fmt.Println("  lifelog           Add timestamped lifelog entry")
	fmt.Println("  create            Create new record in collection")
	fmt.Println()
	fmt.Println("Server mode:")
	fmt.Printf("  tm serve                            Start server on port %s\n", LocalServerPort)
	fmt.Println("                                      Same API as Cloudflare Worker")
	fmt.Println()
	fmt.Println("Config:")
	fmt.Println("  Set THYMER_URL and THYMER_TOKEN environment variables")
	fmt.Println("  Or create ~/.config/tm/config with:")
	fmt.Println("    url=https://thymer.lifelog.my")
	fmt.Println("    token=your-secret-token")
	fmt.Println()
	fmt.Println("  For local development:")
	fmt.Printf("    url=%s\n", LocalServerURL)
	fmt.Println("    token=local-dev-token")
}

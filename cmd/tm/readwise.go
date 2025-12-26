package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	readwiseBaseURL = "https://readwise.io/api/v3/list/"
)

// ReadwiseDocument represents a document from Readwise API
type ReadwiseDocument struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Author          string    `json:"author"`
	Category        string    `json:"category"` // article, book, podcast, etc.
	Summary         string    `json:"summary"`  // LLM-generated summary
	URL             string    `json:"url"`      // Readwise Reader URL
	SourceURL       string    `json:"source_url"`
	ReadingProgress float64   `json:"reading_progress"`
	ParentID        *string   `json:"parent_id"` // Set for highlights
	Content         string    `json:"content"`   // Highlight text if this is a highlight
	Note            string    `json:"note"`      // User's note on highlight
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ReadwiseAPIResponse represents the paginated API response
type ReadwiseAPIResponse struct {
	Count          int                `json:"count"`
	NextPageCursor string             `json:"nextPageCursor"`
	Results        []ReadwiseDocument `json:"results"`
}

// ReadwiseSyncer handles syncing Readwise highlights to Thymer
type ReadwiseSyncer struct {
	token   string
	db      *bolt.DB
	client  *http.Client
}

// NewReadwiseSyncer creates a new Readwise syncer
func NewReadwiseSyncer(token string, dataDir string) (*ReadwiseSyncer, error) {
	dbPath := dataDir + "/readwise.db"
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open readwise db: %w", err)
	}

	// Create buckets
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("documents"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("sync_meta"))
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	return &ReadwiseSyncer{
		token:  token,
		db:     db,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Close closes the database
func (s *ReadwiseSyncer) Close() error {
	return s.db.Close()
}

// Sync fetches documents and highlights, returns documents with new highlights
func (s *ReadwiseSyncer) Sync() ([]HighlightedDocument, error) {
	// Get last sync time
	var lastSync time.Time
	s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("sync_meta"))
		if v := b.Get([]byte("last_sync")); v != nil {
			lastSync, _ = time.Parse(time.RFC3339, string(v))
		}
		return nil
	})

	// Fetch all documents and highlights
	docs, highlights, err := s.fetchAll(lastSync)
	if err != nil {
		return nil, err
	}

	// Group highlights by parent document
	highlightsByDoc := make(map[string][]ReadwiseDocument)
	for _, h := range highlights {
		if h.ParentID != nil {
			highlightsByDoc[*h.ParentID] = append(highlightsByDoc[*h.ParentID], h)
		}
	}

	// Filter to only documents that have highlights
	var results []HighlightedDocument
	for _, doc := range docs {
		docHighlights, hasHighlights := highlightsByDoc[doc.ID]
		if !hasHighlights {
			continue
		}

		// Check if this is new or has new highlights
		isNew, hasNewHighlights := s.checkIfNew(doc.ID, docHighlights)

		if isNew || hasNewHighlights {
			results = append(results, HighlightedDocument{
				Document:   doc,
				Highlights: docHighlights,
				IsNew:      isNew,
			})
		}

		// Store document state
		s.storeDocState(doc.ID, docHighlights)
	}

	// Update last sync time
	s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("sync_meta"))
		return b.Put([]byte("last_sync"), []byte(time.Now().Format(time.RFC3339)))
	})

	return results, nil
}

// HighlightedDocument is a document with its highlights
type HighlightedDocument struct {
	Document   ReadwiseDocument
	Highlights []ReadwiseDocument
	IsNew      bool // First time seeing this document
}

// ToMarkdown converts to frontmatter + markdown body
func (hd *HighlightedDocument) ToMarkdown() string {
	var b strings.Builder

	// Frontmatter
	b.WriteString("---\n")
	b.WriteString("collection: Readwise\n")
	b.WriteString(fmt.Sprintf("external_id: readwise_%s\n", hd.Document.ID))
	if hd.IsNew {
		b.WriteString("verb: highlighted\n")
	}
	b.WriteString(fmt.Sprintf("title: %s\n", cleanTitle(hd.Document.Title)))
	if hd.Document.Author != "" {
		b.WriteString(fmt.Sprintf("author: %s\n", hd.Document.Author))
	}
	b.WriteString(fmt.Sprintf("category: %s\n", hd.Document.Category))
	if hd.Document.SourceURL != "" {
		b.WriteString(fmt.Sprintf("source_url: %s\n", hd.Document.SourceURL))
	}
	if hd.Document.URL != "" {
		b.WriteString(fmt.Sprintf("url: %s\n", hd.Document.URL))
	}
	b.WriteString("---\n\n")

	// Summary section
	if hd.Document.Summary != "" {
		b.WriteString("## Summary\n\n")
		b.WriteString(hd.Document.Summary)
		b.WriteString("\n\n")
	}

	// Highlights section
	if len(hd.Highlights) > 0 {
		b.WriteString("## Highlights\n\n")
		for _, h := range hd.Highlights {
			// Blockquote the highlight
			b.WriteString("> ")
			b.WriteString(strings.ReplaceAll(h.Content, "\n", "\n> "))
			b.WriteString("\n")

			// Add note if present
			if h.Note != "" {
				b.WriteString("\n**Note:** ")
				b.WriteString(h.Note)
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (s *ReadwiseSyncer) fetchAll(since time.Time) (docs []ReadwiseDocument, highlights []ReadwiseDocument, err error) {
	var pageCursor string

	for {
		// Build request URL
		reqUrl := readwiseBaseURL + "?"
		if !since.IsZero() {
			reqUrl += "updatedAfter=" + url.QueryEscape(since.Format(time.RFC3339)) + "&"
		}
		if pageCursor != "" {
			reqUrl += "pageCursor=" + pageCursor
		}

		req, err := http.NewRequest("GET", reqUrl, nil)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Authorization", "Token "+s.token)

		resp, err := s.client.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode == 429 {
			// Rate limited - wait and retry
			retryAfter := resp.Header.Get("Retry-After")
			wait := 60 * time.Second
			if retryAfter != "" {
				if secs, err := time.ParseDuration(retryAfter + "s"); err == nil {
					wait = secs
				}
			}
			fmt.Printf("📚 Readwise rate limited, waiting %v...\n", wait)
			time.Sleep(wait)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("readwise API returned %d: %s", resp.StatusCode, string(body))
		}

		var apiResp ReadwiseAPIResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
			return nil, nil, err
		}

		// Separate documents from highlights
		for _, item := range apiResp.Results {
			if item.ParentID != nil {
				highlights = append(highlights, item)
			} else {
				docs = append(docs, item)
			}
		}

		if apiResp.NextPageCursor == "" {
			break
		}
		pageCursor = apiResp.NextPageCursor
	}

	return docs, highlights, nil
}

func (s *ReadwiseSyncer) checkIfNew(docID string, highlights []ReadwiseDocument) (isNew bool, hasNewHighlights bool) {
	var stored storedDoc

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("documents"))
		v := b.Get([]byte(docID))
		if v == nil {
			isNew = true
			return nil
		}
		return json.Unmarshal(v, &stored)
	})

	if err != nil || isNew {
		return isNew, false
	}

	// Check if we have new highlights
	for _, h := range highlights {
		if !stored.HighlightIDs[h.ID] {
			hasNewHighlights = true
			break
		}
	}

	return false, hasNewHighlights
}

type storedDoc struct {
	HighlightIDs map[string]bool `json:"highlight_ids"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

func (s *ReadwiseSyncer) storeDocState(docID string, highlights []ReadwiseDocument) {
	stored := storedDoc{
		HighlightIDs: make(map[string]bool),
		UpdatedAt:    time.Now(),
	}
	for _, h := range highlights {
		stored.HighlightIDs[h.ID] = true
	}

	data, _ := json.Marshal(stored)
	s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("documents"))
		return b.Put([]byte(docID), data)
	})
}

func cleanTitle(s string) string {
	// Remove characters that could break YAML
	s = strings.ReplaceAll(s, ":", " -")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

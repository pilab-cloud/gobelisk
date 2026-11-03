package voiceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Entry is one line of the conversation transcript.
type Entry struct {
	At        time.Time `json:"at"`
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	LatencyMS int64     `json:"latency_ms,omitempty"`
}

// Transcript collects the conversation in memory and persists it as JSON and
// plain text.
type Transcript struct {
	mu        sync.Mutex
	started   time.Time
	sessionID string
	entries   []Entry
}

// NewTranscript starts a transcript for one meeting session.
func NewTranscript(sessionID string) *Transcript {
	return &Transcript{started: time.Now(), sessionID: sessionID}
}

// Add appends one turn.
func (t *Transcript) Add(role, text string, latencyMS int64) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = append(t.entries, Entry{At: time.Now(), Role: role, Text: text, LatencyMS: latencyMS})
}

// Entries returns a copy of the collected turns.
func (t *Transcript) Entries() []Entry {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Entry, len(t.entries))
	copy(out, t.entries)
	return out
}

// FullText renders the transcript as speaker-labelled lines.
func (t *Transcript) FullText() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var b strings.Builder
	for _, e := range t.entries {
		label := e.Role
		switch e.Role {
		case "assistant":
			label = "Assistant"
		case "user":
			label = "User"
		case "system":
			label = "System"
		}
		fmt.Fprintf(&b, "[%s] %s: %s\n", e.At.Format(time.RFC3339), label, e.Text)
	}
	return b.String()
}

// Save writes <session>.json and <session>.txt under dir and returns the text
// file path.
func (t *Transcript) Save(dir string) (string, error) {
	if dir == "" {
		dir = "transcripts"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := sanitizeName(t.sessionID)
	if name == "" {
		name = fmt.Sprintf("agent-%d", t.started.Unix())
	}
	base := filepath.Join(dir, name)

	entries := t.Entries()
	payload := struct {
		Session string    `json:"session"`
		Started time.Time `json:"started"`
		Ended   time.Time `json:"ended"`
		Entries []Entry   `json:"entries"`
	}{Session: t.sessionID, Started: t.started, Ended: time.Now(), Entries: entries}

	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(base+".json", raw, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(base+".txt", []byte(t.FullText()), 0o644); err != nil {
		return "", err
	}
	return base + ".txt", nil
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// Exporter publishes the finished transcript to an external system.
type Exporter interface {
	Export(ctx context.Context, sessionID string, entries []Entry, transcriptPath string) error
}

// RecorderExporter writes the transcript to the Pir² recorder service as a
// completed transcription job.
type RecorderExporter struct {
	BaseURL     string
	Token       string
	RecordingID string
	Language    string
	Client      *http.Client
}

// Export implements Exporter. The recorder stores agent transcripts directly
// as completed transcription jobs, so no recording (and no extra SFU
// capturer) is created for them.
func (e *RecorderExporter) Export(ctx context.Context, sessionID string, entries []Entry, transcriptPath string) error {
	if strings.TrimSpace(e.BaseURL) == "" {
		return fmt.Errorf("recorder url not configured")
	}
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	base := strings.TrimRight(e.BaseURL, "/")

	recordingID := e.RecordingID
	if recordingID == "" {
		recordingID = "agent-" + sessionID
	}

	language := e.Language
	if language == "" {
		language = "en"
	}

	var full strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&full, "[%s] %s: %s\n", entry.At.Format(time.RFC3339), entry.Role, entry.Text)
	}

	var job struct {
		ID string `json:"id"`
	}
	err := e.doJSON(ctx, client, http.MethodPost, base+"/v1/transcriptions", map[string]string{
		"recording_id":   recordingID,
		"language":       language,
		"transcript":     full.String(),
		"transcript_uri": transcriptPath,
	}, &job)
	if err != nil {
		return fmt.Errorf("recorder create transcription: %w", err)
	}
	if job.ID == "" {
		return fmt.Errorf("recorder did not return a transcription id")
	}
	return nil
}

func (e *RecorderExporter) doJSON(ctx context.Context, client *http.Client, method, url string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.Token != "" {
		req.Header.Set("Authorization", "Bearer "+e.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

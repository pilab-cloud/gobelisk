package meeting

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ResolvedMeeting is the result of resolving a dial-in access code.
type ResolvedMeeting struct {
	MeetingID        string
	MeetingSessionID string
}

// Client calls the Pir² Meeting Service.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

// ResolveCallableMeeting resolves an access code to a meeting/session.
func (c *Client) ResolveCallableMeeting(accessCode string) (*ResolvedMeeting, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("meeting service url not configured")
	}
	body, _ := json.Marshal(map[string]string{"accessCode": accessCode})
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/pir2.meeting.v1.MeetingService/ResolveCallableMeeting", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("meeting service: %d", resp.StatusCode)
	}
	var out struct {
		Meeting struct {
			MeetingId string `json:"meetingId"`
		} `json:"meeting"`
		Session struct {
			MeetingSessionId string `json:"meetingSessionId"`
		} `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Meeting.MeetingId == "" {
		return nil, fmt.Errorf("meeting not found")
	}
	return &ResolvedMeeting{
		MeetingID:        out.Meeting.MeetingId,
		MeetingSessionID: out.Session.MeetingSessionId,
	}, nil
}

package voicemail

import (
	"fmt"
	"net/smtp"
	"os"
	"path/filepath"
)

// Message represents a voicemail.
type Message struct {
	ID         string
	From       string
	To         string
	AudioPath  string
	Transcript string
}

// Store stores voicemails on disk.
type Store struct {
	Dir string
}

func NewStore(dir string) *Store {
	os.MkdirAll(dir, 0755)
	return &Store{Dir: dir}
}

// Save stores a voicemail message metadata.
func (s *Store) Save(msg *Message) error {
	path := filepath.Join(s.Dir, msg.ID+".txt")
	content := fmt.Sprintf("From: %s\nTo: %s\nAudio: %s\nTranscript: %s\n", msg.From, msg.To, msg.AudioPath, msg.Transcript)
	return os.WriteFile(path, []byte(content), 0644)
}

// SendEmail sends a voicemail notification via SMTP.
func SendEmail(smtpHost, smtpPort, from, to, subject, body string) error {
	addr := smtpHost + ":" + smtpPort
	msg := []byte("To: " + to + "\r\n" +
		"From: " + from + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"\r\n" +
		body + "\r\n")
	return smtp.SendMail(addr, nil, from, []string{to}, msg)
}

package voicemail

import (
	"fmt"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Message represents a voicemail.
type Message struct {
	ID          string
	From        string
	To          string
	AudioPath   string
	Transcript  string
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

// Transcribe uses whisper-cli for speech-to-text.
func Transcribe(audioPath string) (string, error) {
	model := "/home/paalgyula/wspace/poc/whisper.cpp/models/for-tests-ggml-small.bin"
	cmd := exec.Command("whisper-cli", "-m", model, "-f", audioPath, "-l", "en", "--no-timestamps")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// TTS generates audio using espeak-ng (Hungarian).
func TTS(text, outputPath string) error {
	tmp := outputPath + ".raw.wav"
	if err := exec.Command("espeak-ng", "-v", "en", "-w", tmp, text).Run(); err != nil {
		return err
	}
	// Convert to 16kHz mono PCM for whisper
	return exec.Command("ffmpeg", "-y", "-i", tmp, "-ar", "16000", "-ac", "1", outputPath).Run()
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
package voicemail

import (
	"bytes"
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

// Transcribe uses whisper-cli (or whisper-cli-gpu) for speech-to-text.
func Transcribe(audioPath string) (string, error) {
	return TranscribeWithConfig(audioPath, "", "", "hu")
}

// TranscribeWithConfig transcribes audioPath using configurable model, device and language.
func TranscribeWithConfig(audioPath, modelPath, device, language string) (string, error) {
	model := modelPath
	if model == "" {
		model = "/opt/whisper/models/ggml-tiny.bin"
		if q5Info, err := os.Stat("/opt/whisper/models/ggml-large-v3-turbo-q5_0.bin"); err == nil && q5Info.Size() > 500_000_000 {
			model = "/opt/whisper/models/ggml-large-v3-turbo-q5_0.bin"
		} else {
			reqModel := strings.ToLower(strings.TrimSpace(os.Getenv("WHISPER_MODEL")))
			if reqModel == "large" || reqModel == "large-v3-turbo" {
				if info, err := os.Stat("/opt/whisper/models/ggml-large-v3-turbo.bin"); err == nil && info.Size() > 1_500_000_000 {
					model = "/opt/whisper/models/ggml-large-v3-turbo.bin"
				}
			}
		}
	}
	if language == "" {
		language = "hu"
	}
	cliBin := "whisper-cli"
	var args []string
	if _, err := exec.LookPath("whisper-cli-gpu"); err == nil {
		cliBin = "whisper-cli-gpu"
		dev := device
		if dev == "" {
			dev = "1"
		}
		args = append(args, "-dev", dev)
	}
	args = append(args,
		"-m", model,
		"-f", audioPath,
		"-l", language,
		"--no-timestamps",
		"-np",
		"-t", "4",
		"-bs", "1",
		"-bo", "1",
		"-nf",
	)
	cmd := exec.Command(cliBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}
	return strings.TrimSpace(string(out)), nil
}

// TTS generates audio using configured synthesizer.
// Voices available: anna, berta, imre
func TTS(text, outputPath string) error {
	return TTSVoice(text, outputPath, "anna")
}

func TTSVoice(text, outputPath, voice string) error {
	return TTSWithConfig(text, outputPath, "piper", "", voice)
}

// TTSWithConfig generates audio using configurable command, model path, and voice.
func TTSWithConfig(text, outputPath, command, modelPath, voice string) error {
	if command == "" {
		command = "piper"
	}
	if command == "piper" {
		if _, err := exec.LookPath("piper"); err == nil {
			model := modelPath
			if model == "" {
				v := voice
				if v == "" {
					v = "anna"
				}
				model = "/opt/piper/models/hu_HU-" + v + "-medium.onnx"
			}
			cmd := exec.Command("piper", "--model", model, "--output_file", outputPath)
			cmd.Stdin = strings.NewReader(text)
			return cmd.Run()
		}
	}
	tmp := outputPath + ".raw.wav"
	if err := exec.Command("espeak-ng", "-v", "hu", "-w", tmp, text).Run(); err != nil {
		return err
	}
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
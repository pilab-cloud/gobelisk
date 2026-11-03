package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/devopshaven/gobelisk/pkg/voiceagent"
)

var (
	agentSession     string
	agentMeeting     string
	agentUID         string
	agentDisplayName string
	agentLanguage    string
	agentGrant       string
	agentSignaling   string
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "run the Pir² voice agent",
	Long:  "The voice agent joins a Pir² meeting over WebRTC, listens to the meeting audio and answers with GPU-synthesized speech.",
}

var agentRunCmd = &cobra.Command{
	Use:   "run",
	Short: "join a meeting and start the voice agent",
	Run:   runVoiceAgent,
}

func runVoiceAgent(cmd *cobra.Command, args []string) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	va := cfg.VoiceAgent
	if agentSession != "" {
		va.MeetingSessionID = agentSession
	}
	if agentMeeting != "" {
		va.MeetingID = agentMeeting
	}
	if agentUID != "" {
		va.UID = agentUID
	}
	if agentDisplayName != "" {
		va.DisplayName = agentDisplayName
	}
	if agentLanguage != "" {
		va.Language = agentLanguage
		va.ASR.Language = agentLanguage
	}
	va.ApplyDefaults()

	if va.MeetingSessionID == "" {
		fmt.Fprintln(os.Stderr, "error: a meeting session id is required (voice_agent.meeting_session_id or --session)")
		os.Exit(1)
	}

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sfu := voiceagent.NewSFUClient(va, logger)
	pipeline := voiceagent.NewPipeline(va, logger)
	sfu.OnAudio(pipeline.HandlePCM)
	pipeline.SetSpeaker(sfu)
	if exporter := voiceagent.RecorderExporterFromConfig(va); exporter != nil {
		pipeline.SetExporter(exporter)
	}

	connect := func() error {
		if agentGrant != "" {
			return sfu.Join(ctx, agentGrant, agentSignaling)
		}
		return sfu.Connect(ctx)
	}
	if err := connect(); err != nil {
		logger.Error().Err(err).Msg("voice agent failed to join")
		os.Exit(1)
	}
	defer sfu.Close()

	readyCtx, readyCancel := context.WithTimeout(ctx, 20*time.Second)
	err = sfu.WaitReady(readyCtx)
	readyCancel()
	if err != nil {
		logger.Error().Err(err).Msg("voice agent media connection did not become ready")
		os.Exit(1)
	}

	logger.Info().Msg("voice agent running; press Ctrl-C to leave")

	pipelineDone := make(chan struct{})
	go func() {
		defer close(pipelineDone)
		pipeline.Run(ctx)
	}()
	<-ctx.Done()

	select {
	case <-pipelineDone:
	case <-time.After(5 * time.Second):
		logger.Warn().Msg("voice agent pipeline did not stop in time")
	}

	logger.Info().Msg("voice agent leaving meeting")
	pipeline.Close(context.Background())
}

var agentInviteCmd = &cobra.Command{
	Use:   "invite",
	Short: "mint a join grant for the agent",
	Long:  "Invites the voice agent to a meeting by minting a short-lived SFU join grant through the Pir² media control service.",
	Run:   runVoiceAgentInvite,
}

func runVoiceAgentInvite(cmd *cobra.Command, args []string) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	va := cfg.VoiceAgent
	if agentSession != "" {
		va.MeetingSessionID = agentSession
	}
	if agentUID != "" {
		va.UID = agentUID
	}
	if agentDisplayName != "" {
		va.DisplayName = agentDisplayName
	}
	va.ApplyDefaults()
	if va.MeetingSessionID == "" {
		fmt.Fprintln(os.Stderr, "error: a meeting session id is required (--session)")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	grant, signaling, err := voiceagent.IssueGrant(ctx, va)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	// The media service returns the browser-facing "/ws" path; the agent
	// connects to the SFU directly, so prefer the configured URL.
	if strings.TrimSpace(va.SFUSignalingURL) != "" &&
		(signaling == "" || strings.HasPrefix(signaling, "/")) {
		signaling = va.SFUSignalingURL
	}

	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{
		"grant":         grant,
		"signaling_url": signaling,
		"session_id":    va.MeetingSessionID,
		"uid":           va.UID,
		"display_name":  va.DisplayName,
		"run_command":   "gobelisk agent run --session " + va.MeetingSessionID + " --grant " + grant + " --signaling-url " + signaling,
	})
}

func init() {
	agentInviteCmd.Flags().StringVar(&agentSession, "session", "", "Pir² meeting session id")
	agentInviteCmd.Flags().StringVar(&agentUID, "uid", "", "SFU peer id / grant subject")
	agentInviteCmd.Flags().StringVar(&agentDisplayName, "display-name", "", "name shown to meeting participants")

	agentRunCmd.Flags().StringVar(&agentSession, "session", "", "Pir² meeting session id")
	agentRunCmd.Flags().StringVar(&agentMeeting, "meeting", "", "Pir² meeting id (used for transcript export)")
	agentRunCmd.Flags().StringVar(&agentUID, "uid", "", "SFU peer id / grant subject")
	agentRunCmd.Flags().StringVar(&agentDisplayName, "display-name", "", "name shown to meeting participants")
	agentRunCmd.Flags().StringVar(&agentLanguage, "language", "", "conversation language (default hu)")
	agentRunCmd.Flags().StringVar(&agentGrant, "grant", "", "pre-issued Pir² join grant (skips the media API)")
	agentRunCmd.Flags().StringVar(&agentSignaling, "signaling-url", "", "SFU signaling URL to use with --grant")

	agentCmd.AddCommand(agentRunCmd)
	agentCmd.AddCommand(agentInviteCmd)
	rootCmd.AddCommand(agentCmd)
}

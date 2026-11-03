/*
Copyright © 2024-2026 Made with <3 by Progressive Innovation LAB - Paal Gyula <gyula@pilab.hu>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path"

	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/devopshaven/gobelisk/pkg/dialplan"
	"github.com/devopshaven/gobelisk/pkg/ivr"
	"github.com/devopshaven/gobelisk/pkg/pbx"
	"github.com/devopshaven/gobelisk/pkg/rooms"
	"github.com/devopshaven/gobelisk/pkg/sip"
	"github.com/devopshaven/gobelisk/pkg/sip/auth"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gobelisk [flags] [command] [args]",
	Short: "very simple SIP PBX Server written in golang",
	Long: `gobelisk is a new PBX implementation in Go. It's very simple, and its goal is to be platform-independent.

		For more information, please visit https://github.com/devopshaven/gobelisk
	`,
	Run: StartServer,
}

func StartServer(cmd *cobra.Command, args []string) {
	fmt.Println("Starting gobelisk SIP server...")

	// Sets up packet dump
	sip.DumpPackets(true)

	// create pretty console logger
	zerolog.SetGlobalLevel(zerolog.TraceLevel)

	// create default logger
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		With().Caller().Timestamp().Logger()

	cfg, err := config.Load(cfgFile)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	var authenticator auth.Authenticator = auth.NoneAuthenticator{}
	if cfg.Auth.Type == "digest" {
		authenticator = auth.BuildDefaultAuthenticator()
	}

	dp := dialplan.New()
	ivrMgr := ivr.NewManager()
	roomMgr := rooms.NewManager()
	if cfg.DialPlan != "" {
		if err := dialplan.ApplyConfig(dp, ivrMgr, cfg.DialPlan); err != nil {
			log.Warn().Err(err).Msg("failed to apply dialplan config")
		}
	}

	server := pbx.NewServer(
		pbx.WithLogger(log),
		pbx.WithAuthenticator(authenticator),
		pbx.WithDialPlan(dp),
		pbx.WithRooms(roomMgr),
		pbx.WithIVR(ivrMgr),
		pbx.WithConfig(cfg),
		pbx.WithConfigPath(cfgFile),
		pbx.WithRPCPort(cfg.Server.GRPCPort),
	)
	server.Start()

	go server.ListenUDP(cfg.Server.UDPPort)
	go server.ListenTCP(cfg.Server.TCPPort)
	go server.ListenTLS(cfg.Server.TLSPort)
	go func() {
		if err := server.StartRPC(); err != nil {
			log.Error().Err(err).Msg("rpc server error")
		}
	}()

	// Wait for SIGINT
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	<-signals

	log.Info().Msg("Signal received, shutting down the server...")

	// Graceful stop the server
	server.Close()
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.gobelisk/config.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	// rootCmd.Flags().BoolP("toggle", "i", false, "Help message for toggle")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name "$home/.gobelisk/config.yaml" (without extension).
		viper.AddConfigPath(path.Join(home, ".gobelisk"))
		viper.SetConfigType("yaml")
		viper.SetConfigName(".config")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

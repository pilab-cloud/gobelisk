package cli

import (
	"strings"

	"github.com/chzyer/readline"
	"github.com/rs/zerolog/log"
)

var commands = []string{
	"configure terminal",
	"enable",
	"exit",
	"show running-config",
	"show ip interface brief",
}

func RunShell() {
	rl, err := readline.New("> ")
	if err != nil {
		panic(err)
	}
	defer rl.Close()

	rl.Config.AutoComplete = readline.NewPrefixCompleter(
		readline.PcItem("configure",
			readline.PcItem("terminal"),
		),
		readline.PcItem("enable"),
		readline.PcItem("exit"),
		readline.PcItem("show",
			readline.PcItem("running-config"),
			readline.PcItem("ip",
				readline.PcItem("interface",
					readline.PcItem("brief"),
				),
			),
		),
		readline.PcItem("copy",
			readline.PcItem("running-config",
				readline.PcItem("startup-config"),
			),
		),
	)

	for {
		line, err := rl.Readline()
		if err != nil { // io.EOF
			break
		}

		line = strings.TrimSpace(line)

		// TODO: parse and execute
		log.Debug().Msg(line)

		if line == "" {
			continue
		}

		log.Info().Msg(line)

		// TODO: handle exit
		if line == "exit" {
			break
		}
	}
}

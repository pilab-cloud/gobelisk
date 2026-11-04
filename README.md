# Gobelisk - The PBX and SIP toolkit in go

[![Go Report Card](https://goreportcard.com/badge/github.com/pilab-cloud/gobelisk)](https://goreportcard.com/report/github.com/pilab-cloud/gobelisk)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/pilab-cloud/gobelisk)](https://golang.org/)
[![Release](https://img.shields.io/github/v/release/pilab-cloud/gobelisk?include_prereleases)](https://github.com/pilab-cloud/gobelisk/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/pilab-cloud/gobelisk/release.yml?branch=master)](https://github.com/pilab-cloud/gobelisk/actions)
[![Last Commit](https://img.shields.io/github/last-commit/pilab-cloud/gobelisk)](https://github.com/pilab-cloud/gobelisk/commits/master)
[![Discord](https://img.shields.io/badge/Discord-Join%20Us-7289da.svg)](https://discord.gg/)

> **The future of telephony. Written in Go. Built for the modern era.**

<p align="center">
<img src="docs/gopher.png" height="350"/>
</p>

Gobelisk is a free, modern, and extensible PBX server built from the ground up in pure Go. Inspired by Asterisk, but reimagined for simplicity, performance, and cloud-native deployments. Say goodbye to decades of legacy complexity and hello to a sleek, developer-friendly communication platform.

## ✨ Why Gobelisk?

- **🚀 Blazing Fast** - Written in Go for unmatched performance and low latency
- **🎯 Modern Architecture** - Clean, modular design that's easy to extend and maintain
- **🔌 SIP Native** - Full RFC-compliant SIP stack built from scratch
- **💬 Pir² Integration** - Seamless integration with Pir² meeting platform for hybrid communication
- **🛠️ Developer Friendly** - Simple configuration, gRPC APIs, and powerful CLI tools
- **🌐 Cloud Ready** - Containerized, horizontally scalable, and perfect for modern infrastructure
- **📡 Extensible** - Plugin architecture with support for custom applications and dialplans

## 🎯 Features

| Feature                     | Status    | Description                                                      |
| --------------------------- | --------- | ---------------------------------------------------------------- |
| **SIP Stack**               | ✅ Stable | RFC 3261 compliant SIP implementation with UDP/TCP/TLS support   |
| **Call Routing**            | ✅ Stable | Flexible dialplan with wildcard matching and intelligent routing |
| **IVR Menus**               | ✅ Stable | Interactive Voice Response with DTMF collection                  |
| **Extension Management**    | ✅ Stable | Full CRUD operations via gRPC API                                |
| **Room/Conference Support** | ✅ Stable | Basic conferencing with participant tracking                     |
| **Voicemail**               | 🟡 Beta   | Voicemail infrastructure; speech via the pluggable engine         |
| **Call Transfer**           | 🚧 WIP    | Attended and blind transfer capabilities                         |
| **Call Center (ACD)**       | 🚧 WIP    | Queue management and agent routing                               |
| **Trunk Management**        | 🚧 WIP    | SIP trunk provisioning and routing                               |
| **Recording & Analytics**   | 🚧 WIP    | Call recording and detailed analytics                            |

## 🚀 Quick Start

### Prerequisites

- Go 1.26+
- (Optional) Docker for containerized deployment

### Installation

#### From Source

```bash
# Clone the repository
git clone https://github.com/pilab-cloud/gobelisk.git
cd gobelisk

# Build
go build -o gobelisk .

# Run
./gobelisk server
```

#### Using Go Install

```bash
go install github.com/pilab-cloud/gobelisk@latest
```

### Basic Usage

```bash
# Start the PBX server
gobelisk server

# Manage extensions via CLI
gobelisk manage extension add 1001 --password secret

# Access the shell for interactive management
gobelisk shell

# Launch web interface
gobelisk web
```

## 🏗️ Architecture

Gobelisk is built with a modular architecture, separating concerns for maximum flexibility:

- **`pkg/sip/`** - Complete SIP protocol implementation
- **`pkg/pbx/`** - Core PBX server logic and call handling
- **`pkg/dialplan/`** - Call routing and dialplan engine
- **`pkg/ivr/`** - Interactive Voice Response system
- **`pkg/rooms/`** - Conference room management
- **`pkg/speech/`** - Pluggable speech-to-text / text-to-speech engine (no-op by default)
- **`cmd/`** - CLI commands and entry points
- **`proto/`** - gRPC API definitions

### Speech engine (optional)

The core binary performs no AI work. `pkg/speech` defines a small pluggable
interface and ships a no-op implementation:

```go
type Engine interface {
    Transcribe(audioPath string) (string, error)
    Synthesize(text, outputPath, voice string) error
}
```

An implementation registers itself under a name and is selected in
`gobelisk.yaml`:

```yaml
speech:
  engine: whisper-piper
  options: { ... }
```

The reference implementation (Whisper ASR + Piper TTS) lives in the separate
[gobelisk-ai](https://github.com/pilab-cloud/gobelisk-ai) project. It builds its
own binary that blank-imports the engine package and reuses this CLI; embedders
can also inject an engine directly with `pbx.WithSpeechEngine(...)`.


## 🤝 Contributing

We welcome contributions from the community! Whether you're fixing bugs, adding features, or improving documentation, your help is appreciated.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

Please see our [contributing guidelines](CONTRIBUTING.md) for more details.

## 📜 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 💖 Credits

**Made with ❤️ by Progressive Innovation LAB - Paál Gyula <gyula@pilab.hu>**

Gobelisk is proudly developed and maintained by the team at [Progressive Innovation LAB](https://pilab.hu). We believe in building open-source tools that empower developers and businesses to communicate freely.

---

_Gobelisk - Telephony reimagined for the modern world._

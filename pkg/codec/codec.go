package codec

import (
	"strings"
)

// Supported codecs
var Supported = []string{"PCMU", "PCMA", "G722", "telephone-event"}

// ExtractCodecs returns codec names from an SDP body.
func ExtractCodecs(sdp string) []string {
	var out []string
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "a=rtpmap:") {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) < 2 {
				continue
			}
			enc := parts[1]
			if idx := strings.Index(enc, "/"); idx > 0 {
				enc = enc[:idx]
			}
			out = append(out, enc)
		}
	}
	return out
}

// Transform rewrites SDP to only include preferred codecs.
func Transform(sdp string, preferred []string) string {
	// Simple: keep all lines; could filter rtpmap lines not in preferred.
	// For now return as-is (passthrough) but provide hook.
	return sdp
}
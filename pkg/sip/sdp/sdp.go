package sdp

// Session description
//          v=  (protocol version)
//          o=  (originator and session identifier)
//          s=  (session name)
//          i=* (session information)
//          u=* (URI of description)
//          e=* (email address)
//          p=* (phone number)
//          c=* (connection information -- not required if included in
//               all media descriptions)
//          b=* (zero or more bandwidth information lines)
//          One or more time descriptions:
//            ("t=", "r=" and "z=" lines; see below)
//          k=* (obsolete)
//          a=* (zero or more session attribute lines)
//          Zero or more media descriptions

//       Time description
//          t=  (time the session is active)
//          r=* (zero or more repeat times)
//          z=* (optional time zone offset line)

//       Media description, if present
//          m=  (media name and transport address)
//          i=* (media title)
//          c=* (connection information -- optional if included at
//               session level)
//          b=* (zero or more bandwidth information lines)
//          k=* (obsolete)
//          a=* (zero or more media attribute lines)

import (
	"fmt"
	"strings"
)

// SDP represents a Session Description Protocol structure.
type SDP struct {
	Version           string             // v protocol version
	Origin            string             // o originator and session identifier
	SessionName       string             // s session name
	SessionInfo       string             // i* session information
	URI               string             // u* URI of description
	Email             string             // e* email address
	Phone             string             // p* phone number
	Connection        string             // c* connection information -- not required if included in all media descriptions
	Bandwidth         []string           // b* zero or more bandwidth information lines
	TimeDescriptions  []TimeDescription  // t, r*, z* zero or more time descriptions
	Attributes        []string           // a* zero or more session attribute lines
	MediaDescriptions []MediaDescription // m, i*, c*, b*, a* zero or more media descriptions
}

// TimeDescription represents a time description in an SDP.
type TimeDescription struct {
	ActiveTime  string   // t=
	RepeatTimes []string // r=*
	TimeZones   string   // z=*
}

// MediaDescription represents a media description in an SDP.
type MediaDescription struct {
	MediaName  string   // m=
	MediaTitle string   // i=*
	Connection string   // c=*
	Bandwidth  []string // b=*
	Attributes []string // a=*
}

// MarshalSDP marshals an SDP struct into a string.
func MarshalSDP(sdp SDP) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("v=%s\n", sdp.Version))
	sb.WriteString(fmt.Sprintf("o=%s\n", sdp.Origin))
	sb.WriteString(fmt.Sprintf("s=%s\n", sdp.SessionName))
	if sdp.SessionInfo != "" {
		sb.WriteString(fmt.Sprintf("i=%s\n", sdp.SessionInfo))
	}

	if sdp.URI != "" {
		sb.WriteString(fmt.Sprintf("u=%s\n", sdp.URI))
	}

	if sdp.Email != "" {
		sb.WriteString(fmt.Sprintf("e=%s\n", sdp.Email))
	}

	if sdp.Phone != "" {
		sb.WriteString(fmt.Sprintf("p=%s\n", sdp.Phone))
	}

	if sdp.Connection != "" {
		sb.WriteString(fmt.Sprintf("c=%s\n", sdp.Connection))
	}

	for _, b := range sdp.Bandwidth {
		sb.WriteString(fmt.Sprintf("b=%s\n", b))
	}

	for _, td := range sdp.TimeDescriptions {
		sb.WriteString(fmt.Sprintf("t=%s\n", td.ActiveTime))
		for _, r := range td.RepeatTimes {
			sb.WriteString(fmt.Sprintf("r=%s\n", r))
		}
		if td.TimeZones != "" {
			sb.WriteString(fmt.Sprintf("z=%s\n", td.TimeZones))
		}
	}

	for _, a := range sdp.Attributes {
		sb.WriteString(fmt.Sprintf("a=%s\n", a))
	}

	for _, md := range sdp.MediaDescriptions {
		sb.WriteString(fmt.Sprintf("m=%s\n", md.MediaName))

		if md.MediaTitle != "" {
			sb.WriteString(fmt.Sprintf("i=%s\n", md.MediaTitle))
		}

		if md.Connection != "" {
			sb.WriteString(fmt.Sprintf("c=%s\n", md.Connection))
		}

		for _, b := range md.Bandwidth {
			sb.WriteString(fmt.Sprintf("b=%s\n", b))
		}

		for _, a := range md.Attributes {
			sb.WriteString(fmt.Sprintf("a=%s\n", a))
		}
	}

	return sb.String()
}

// UnmarshalSDP unmarshals a string into an SDP struct.
func UnmarshalSDP(input string) (SDP, error) {
	var sdp SDP
	var currentMedia *MediaDescription
	lines := strings.Split(input, "\n")

	for _, line := range lines {
		if len(line) < 2 {
			continue
		}

		prefix := line[:2]
		value := line[2:]

		switch prefix {
		case "v=":
			sdp.Version = value
		case "o=":
			sdp.Origin = value
		case "s=":
			sdp.SessionName = value
		case "i=":
			if currentMedia != nil {
				currentMedia.MediaTitle = value
			} else {
				sdp.SessionInfo = value
			}
		case "u=":
			sdp.URI = value
		case "e=":
			sdp.Email = value
		case "p=":
			sdp.Phone = value
		case "c=":
			if currentMedia != nil {
				currentMedia.Connection = value
			} else {
				sdp.Connection = value
			}
		case "b=":
			if currentMedia != nil {
				currentMedia.Bandwidth = append(currentMedia.Bandwidth, value)
			} else {
				sdp.Bandwidth = append(sdp.Bandwidth, value)
			}
		case "t=":
			sdp.TimeDescriptions = append(sdp.TimeDescriptions, TimeDescription{ActiveTime: value})
		case "r=":
			if len(sdp.TimeDescriptions) > 0 {
				last := &sdp.TimeDescriptions[len(sdp.TimeDescriptions)-1]
				last.RepeatTimes = append(last.RepeatTimes, value)
			}
		case "z=":
			if len(sdp.TimeDescriptions) > 0 {
				last := &sdp.TimeDescriptions[len(sdp.TimeDescriptions)-1]
				last.TimeZones = value
			}
		case "a=":
			if currentMedia != nil {
				currentMedia.Attributes = append(currentMedia.Attributes, value)
			} else {
				sdp.Attributes = append(sdp.Attributes, value)
			}
		case "m=":
			md := MediaDescription{MediaName: value}
			sdp.MediaDescriptions = append(sdp.MediaDescriptions, md)
			currentMedia = &sdp.MediaDescriptions[len(sdp.MediaDescriptions)-1]
		}
	}

	return sdp, nil
}

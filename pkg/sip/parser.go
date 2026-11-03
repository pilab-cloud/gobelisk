package sip

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// ErrInvalidHeader is returned when a SIP message has an invalid header.
var ErrInvalidHeader = errors.New("invalid header")

// RawMessage represents a parsed SIP message.
type RawMessage struct {
	Method     string            // Request method for requests, empty for responses
	Target     string            // Request target for requests
	URI        string            // Request URI for requests, empty for responses
	SipVersion string            // SIP version
	StatusCode int               // Status code for responses, 0 for requests
	StatusDesc string            // Status description for responses, empty for requests
	Headers    map[string]string // SIP headers
	Body       string            // SIP message body
}

// UnmarshalBytes parses a raw SIP message into a RawMessage struct.
func UnmarshalBytes(rm *RawMessage, bb []byte) error {
	if dumpPackets {
		fmt.Println(string(bb))
	}

	return Unmarshal(rm, bytes.NewReader(bb))
}

// ParseSIPMessage parses a raw SIP message into a SIPMessage struct.
func Unmarshal(rm *RawMessage, r io.Reader) error {
	var scanner *bufio.Scanner

	// Dump packets to stdout
	if dumpPackets {
		var bb bytes.Buffer
		_, _ = io.Copy(&bb, r)
		br := bytes.NewReader(bb.Bytes())

		fmt.Println(bb.String())

		scanner = bufio.NewScanner(br)
	} else {
		scanner = bufio.NewScanner(r)
	}

	// Read the headers
	readBody := false
	var bodyBuffer bytes.Buffer
	var lastHeaderKey string

	// Initialize the headers when nil
	if rm.Headers == nil {
		rm.Headers = make(map[string]string)
	}

	// Read the SIP version
	if scanner.Scan() {
		messageHeader := scanner.Text()
		if err := readHeader(messageHeader, rm); err != nil {
			return fmt.Errorf("sip.Unmarshal: %w", err)
		}
	} else {
		return fmt.Errorf("sip.Unmarshal: %w", scanner.Err())
	}

	// Start reading line-by-line
	for scanner.Scan() {
		line := scanner.Text()

		// If the line is empty, we've reached the end of the headers
		if line == "" {
			readBody = true

			continue
		}

		// Read the message body
		if !readBody {
			// Handle header folding (continuation lines starting with whitespace)
			if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && lastHeaderKey != "" {
				rm.Headers[lastHeaderKey] = rm.Headers[lastHeaderKey] + strings.TrimSpace(line)
				continue
			}
			// Headers
			parts := strings.SplitN(line, ":", 2)

			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				rm.Headers[key] = value
				lastHeaderKey = key
			}
		} else {
			// Body
			bodyBuffer.WriteString(line + "\n")
		}
	}

	// Check for errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading SIP message: %w", err)
	}

	// Parse the body
	rm.Body = bodyBuffer.String()

	return nil
}

// Read the header of SIP message
func readHeader(message string, raw *RawMessage) error {
	if strings.HasPrefix(message, "SIP/") {
		// This is a response
		re := regexp.MustCompile(`^(SIP/\d\.\d) (\d{3}) ([\w\s]+)$`)
		matches := re.FindStringSubmatch(message)

		if len(matches) > 0 {
			statusCode, err := strconv.Atoi(matches[2])
			if err != nil {
				return fmt.Errorf("invalid status code: %w", err)
			}

			raw.SipVersion = matches[1]
			raw.StatusCode = statusCode
			raw.StatusDesc = matches[3]

			return nil
		}

		return ErrInvalidHeader
	}

	// This is a request
	re := regexp.MustCompile(`^(\w+) (.+) (SIP/\d\.\d)$`)
	matches := re.FindStringSubmatch(message)

	if len(matches) > 0 {
		raw.Method = matches[1]
		raw.Target = matches[2]
		raw.URI = matches[2]
		raw.SipVersion = matches[3]

		return nil
	}

	return ErrInvalidHeader
}

package sdp

import (
	"fmt"
	"strconv"
	"strings"
)

type RTPMap struct {
	Value        string
	PayloadType  int // zero-based-integer
	EncodingName string
	ClockRate    int
	Channels     int // channels
}

func NewRTPMap() *RTPMap {
	return &RTPMap{
		Value:        "",
		PayloadType:  0,
		EncodingName: "",
		ClockRate:    0,
		Channels:     0,
	}
}

func (rm *RTPMap) Parse(value string) error {
	// Strip optional "a=rtpmap:" prefix
	if strings.HasPrefix(value, "a=rtpmap:") {
		value = strings.TrimPrefix(value, "a=rtpmap:")
		value = strings.TrimSpace(value)
	}

	parts := strings.Split(value, " ")
	if len(parts) < 2 {
		return fmt.Errorf("invalid rtpmap value")
	}

	payloadType, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("invalid payload type: %v", err)
	}

	encodingNameAndClockRate := strings.Split(parts[1], "/")
	if len(encodingNameAndClockRate) < 2 {
		return fmt.Errorf("invalid encoding name and clock rate format")
	}

	encodingName := encodingNameAndClockRate[0]
	clockRate, err := strconv.Atoi(encodingNameAndClockRate[1])
	if err != nil {
		return fmt.Errorf("invalid clock rate: %v", err)
	}

	encodingParams := 0
	if len(encodingNameAndClockRate) >= 3 {
		encodingParams, err = strconv.Atoi(encodingNameAndClockRate[2])
		if err != nil {
			return fmt.Errorf("invalid encoding params: %v", err)
		}
	}

	rm.PayloadType = payloadType
	rm.EncodingName = encodingName
	rm.ClockRate = clockRate
	rm.Channels = encodingParams

	return nil
}

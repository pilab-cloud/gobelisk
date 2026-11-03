package sdp_test

import (
	"reflect"
	"testing"

	"github.com/devopshaven/gobelisk/pkg/sip/sdp"
)

func TestUnmarshalSDP(t *testing.T) {
	input := `v=0
o=jdoe 3724394400 3724394405 IN IP4 198.51.100.1
s=Call to John Smith
i=SDP Offer #1
u=http://www.jdoe.example.com/home.html
e=Jane Doe <jane@jdoe.example.com>
p=+1 617 555-6011
c=IN IP4 198.51.100.1
t=0 0
m=audio 49170 RTP/AVP 0
a=rtpmap:0 PCMU/8000
m=audio 49180 RTP/AVP 0
m=video 51372 RTP/AVP 99
c=IN IP6 2001:db8::2
a=rtpmap:99 h263-1998/90000`

	expected := sdp.SDP{
		Version:     "0",
		Origin:      "jdoe 3724394400 3724394405 IN IP4 198.51.100.1",
		SessionName: "Call to John Smith",
		SessionInfo: "SDP Offer #1",
		URI:         "http://www.jdoe.example.com/home.html",
		Email:       "Jane Doe <jane@jdoe.example.com>",
		Phone:       "+1 617 555-6011",
		Connection:  "IN IP4 198.51.100.1",
		TimeDescriptions: []sdp.TimeDescription{
			{ActiveTime: "0 0"},
		},
		MediaDescriptions: []sdp.MediaDescription{
			{
				MediaName:  "audio 49170 RTP/AVP 0",
				Attributes: []string{"rtpmap:0 PCMU/8000"},
			},
			{
				MediaName: "audio 49180 RTP/AVP 0",
			},
			{
				MediaName:  "video 51372 RTP/AVP 99",
				Connection: "IN IP6 2001:db8::2",
				Attributes: []string{"rtpmap:99 h263-1998/90000"},
			},
		},
	}

	result, err := sdp.UnmarshalSDP(input)
	if err != nil {
		t.Errorf("UnmarshalSDP returned an error: %v", err)
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Unmarshaled SDP does not match expected.\nGot: %#v\nExpected: %#v", result, expected)
	}
}

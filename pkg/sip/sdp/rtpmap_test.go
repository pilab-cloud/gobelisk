package sdp_test

import (
	"testing"

	"github.com/devopshaven/gobelisk/pkg/sip/sdp"
)

func TestParseRtpMap(t *testing.T) {
	cases := []struct {
		in   string
		want sdp.RTPMap
	}{
		{
			in:   "a=rtpmap:96 L8/8000",
			want: sdp.RTPMap{PayloadType: 96, EncodingName: "L8", ClockRate: 8000},
		},
		{
			in:   "a=rtpmap:97 L16/8000",
			want: sdp.RTPMap{PayloadType: 97, EncodingName: "L16", ClockRate: 8000},
		},
		{
			in:   "a=rtpmap:98 L16/11025/2",
			want: sdp.RTPMap{PayloadType: 98, EncodingName: "L16", ClockRate: 11025, Channels: 2},
		},
	}

	for _, c := range cases {
		var rtpmap sdp.RTPMap
		if err := rtpmap.Parse(c.in); err != nil {
			t.Errorf("Parse(%q) failed: %v", c.in, err)

			continue
		}

		// got := *rtpmap
		if rtpmap != c.want {
			t.Errorf("ParseRtpMap(%q) == %v, want %v", c.in, rtpmap, c.want)
		}
	}
}

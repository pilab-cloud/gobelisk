package sip_test

import (
	"strings"
	"testing"

	"github.com/devopshaven/gobelisk/pkg/sip"
	"github.com/stretchr/testify/assert"
)

// TestParaser tests the parser
func TestParaser(t *testing.T) {
	rawSIPMessage := `INVITE sip:bob@biloxi.com SIP/2.0
Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776asdhds
Max-Forwards: 70
To: Bob <sip:bob@biloxi.com>
From: Alice <sip:alice@atlanta.com>;tag=1928301774
Call-ID: a84b4c76e66710
CSeq: 314159 INVITE
Contact: <sip:alice@pc33.atlanta.com>
Content-Type: application/sdp
Content-Length: 142

v=0
o=alice 2890844526 2890844526 IN IP4 pc33.atlanta.com
s=-
c=IN IP4 pc33.atlanta.com
t=0 0
m=audio 49170 RTP/AVP 0
a=rtpmap:0 PCMU/8000`

	var msg sip.RawMessage
	err := sip.Unmarshal(&msg, strings.NewReader(rawSIPMessage))
	assert.NoError(t, err)

	assert.Equal(t, "INVITE", msg.Method)
	assert.Equal(t, 139, len(msg.Body))
	assert.Equal(t, "142", msg.Headers["Content-Length"])
	assert.Equal(t, "Bob <sip:bob@biloxi.com>", msg.Headers["To"])
	assert.Equal(t, "Alice <sip:alice@atlanta.com>;tag=1928301774", msg.Headers["From"])
	assert.Equal(t, "a84b4c76e66710", msg.Headers["Call-ID"])

}

func TestResponseMessage(t *testing.T) {
	rawSIPMessage := `SIP/2.0 100 Trying
Via: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bKnashds8
	;received=192.0.2.1
To: Bob <sip:bob@biloxi.com>
From: Alice <sip:alice@atlanta.com>;tag=1928301774
Call-ID: a84b4c76e66710
CSeq: 314159 INVITE
Content-Length: 0`

	var msg sip.RawMessage
	err := sip.Unmarshal(&msg, strings.NewReader(rawSIPMessage))
	assert.NoError(t, err)

	assert.Equal(t, "SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bKnashds8;received=192.0.2.1", msg.Headers["Via"])
	assert.Equal(t, "a84b4c76e66710", msg.Headers["Call-ID"])
	assert.Equal(t, "314159 INVITE", msg.Headers["CSeq"])
	assert.Equal(t, "0", msg.Headers["Content-Length"])
	assert.Equal(t, "Bob <sip:bob@biloxi.com>", msg.Headers["To"])
	assert.Equal(t, 100, msg.StatusCode)
	assert.Equal(t, "Trying", msg.StatusDesc)
}

func TestRegisterMessage(t *testing.T) {
	rawmessage := `REGISTER sip:127.0.0.1:15060 SIP/2.0
Via: SIP/2.0/UDP 172.16.1.37:46811;rport;branch=z9hG4bKD9UHZ0tgNKU2H
Max-Forwards: 70
From: <sip:100@127.0.0.1>;tag=FUjy2paHKKNym
To: <sip:100@127.0.0.1>
Call-ID: 4af325c0-4fda-123d-2db4-54e1ad9d65af
CSeq: 79953439 REGISTER
Contact: "100@gobelisk" <sip:100@172.16.1.37:46811;user=phone>;methods="INVITE,ACK,BYE,CANCEL,OPTIONS,PRACK,MESSAGE,SUBSCRIBE,NOTIFY,REFER,UPDATE"
Expires: 180
User-Agent: calls sofia-sip/1.13.17
Allow: INVITE, ACK, BYE, CANCEL, OPTIONS, PRACK, MESSAGE, SUBSCRIBE, NOTIFY, REFER, UPDATE
Supported: timer, 100rel, replaces, gruu, outbound
Content-Length: 0`

	var msg sip.RawMessage
	err := sip.Unmarshal(&msg, strings.NewReader(rawmessage))
	assert.NoError(t, err)

	assert.Equal(t, "REGISTER", msg.Method)
	assert.Equal(t, "sip:127.0.0.1:15060", msg.URI)

}

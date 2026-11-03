package pbx

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	gobeliskv1 "github.com/devopshaven/gobelisk/gen/go/gobelisk/v1"
	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/devopshaven/gobelisk/pkg/sip"
	"github.com/icholy/digest"
	"github.com/rs/xid"
)

// Trunk registration states (GOBELISK-25).
const (
	TrunkStateDisabled    = "disabled"    // enabled: false
	TrunkStateRegistering = "registering" // attempt in flight / to be made
	TrunkStateRegistered  = "registered"  // last register succeeded, not expired
	TrunkStateUnhealthy   = "unhealthy"   // consecutive failures reached the threshold
	TrunkStateUnsupported = "unsupported" // iax protocol or non-UDP transport
)

// trunkFailureThreshold is how many consecutive failed interactions (register
// or OPTIONS probe) turn a trunk unhealthy and raise an alert.
const trunkFailureThreshold = 3

// trunkStatus is a point-in-time snapshot of one trunk's runtime state.
type trunkStatus struct {
	Name         string
	State        string
	Alert        string
	LastError    string
	Failures     int
	ExpiresAt    time.Time
	LastRegister time.Time
}

// trunkState is one provisioned trunk plus its live registration state.
type trunkState struct {
	cfg config.TrunkConfig

	mu             sync.Mutex
	state          string
	alert          string
	lastErr        string
	failures       int
	expiresAt      time.Time
	renewAt        time.Time
	lastRegister   time.Time
	remoteAddr     *net.UDPAddr
	nextCSeq       int
	started        bool
	deregisterOnce sync.Once

	callID  string
	fromTag string
	stop    chan struct{}
}

func newTrunkState(tc config.TrunkConfig) *trunkState {
	return &trunkState{
		cfg:     tc,
		state:   trunkStateFor(tc),
		callID:  tc.Name + "-" + xid.New().String(),
		fromTag: xid.New().String(),
		stop:    make(chan struct{}),
	}
}

// trunkStateFor is the state a trunk starts in, before any attempt runs.
func trunkStateFor(tc config.TrunkConfig) string {
	switch {
	case !tc.RegisterEnabled():
		return TrunkStateDisabled
	case tc.TrunkType() == "iax":
		return TrunkStateUnsupported
	case tc.Transport != "" && !strings.EqualFold(tc.Transport, "udp"):
		return TrunkStateUnsupported
	default:
		return TrunkStateRegistering
	}
}

func (st *trunkState) status() trunkStatus {
	st.mu.Lock()
	defer st.mu.Unlock()
	return trunkStatus{
		Name:         st.cfg.Name,
		State:        st.state,
		Alert:        st.alert,
		LastError:    st.lastErr,
		Failures:     st.failures,
		ExpiresAt:    st.expiresAt,
		LastRegister: st.lastRegister,
	}
}

// registerOK records a successful register: the trunk is registered and all
// counters and alerts are cleared.
func (st *trunkState) registerOK(expires time.Duration) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.state = TrunkStateRegistered
	st.failures = 0
	st.alert = ""
	st.lastErr = ""
	now := time.Now()
	st.lastRegister = now
	st.expiresAt = now.Add(expires)
	st.renewAt = now.Add(expires / 2)
}

// healthOK records a successful OPTIONS probe: counters clear and an
// unhealthy trunk returns to registered, but the register schedule and the
// last-register timestamp stay untouched.
func (st *trunkState) healthOK() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.failures = 0
	st.alert = ""
	st.lastErr = ""
	if st.state == TrunkStateUnhealthy {
		st.state = TrunkStateRegistered
	}
}

// fail records a failed interaction; at the threshold it flips to unhealthy
// and raises the alert (GOBELISK-25: alerts on failure).
func (st *trunkState) fail(err error, alert string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.failures++
	st.lastErr = err.Error()
	if st.failures >= trunkFailureThreshold {
		st.state = TrunkStateUnhealthy
		st.alert = fmt.Sprintf("%s: %s", alert, err.Error())
	}
}

// renewIn returns how long to wait before the next attempt: half the granted
// lifetime while registered, the configured retry delay otherwise.
func (st *trunkState) renewIn() time.Duration {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.state == TrunkStateRegistered && !st.renewAt.IsZero() {
		if d := time.Until(st.renewAt); d > 0 {
			if d < time.Second {
				return time.Second
			}
			return d
		}
	}
	return st.cfg.RetryOrDefault()
}

// cseq returns the next CSeq number for this trunk (loop goroutine only).
func (st *trunkState) cseq() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.nextCSeq++
	return st.nextCSeq
}

// addr resolves (once per call site) and returns the provider address.
func (st *trunkState) addr() *net.UDPAddr {
	st.mu.Lock()
	a := st.remoteAddr
	st.mu.Unlock()
	if a != nil {
		return a
	}
	resolved, err := net.ResolveUDPAddr("udp",
		net.JoinHostPort(st.cfg.Host, strconv.Itoa(st.cfg.PortOrDefault())))
	if err != nil {
		return nil
	}
	st.mu.Lock()
	st.remoteAddr = resolved
	st.mu.Unlock()
	return resolved
}

// ---- outbound requests (GOBELISK-25) ----

// trunkURI is the REGISTER Request-URI: the from-domain (default host).
func (st *trunkState) trunkURI() string {
	uri := "sip:" + st.fromDomain()
	if p := st.cfg.PortOrDefault(); p != 5060 {
		uri += ":" + strconv.Itoa(p)
	}
	return uri
}

func (st *trunkState) fromDomain() string {
	if st.cfg.FromDomain != "" {
		return st.cfg.FromDomain
	}
	return st.cfg.Host
}

func (st *trunkState) user() string {
	if st.cfg.Username != "" {
		return st.cfg.Username
	}
	return "gobelisk"
}

// buildTrunkRequest assembles an outbound REGISTER/OPTIONS for a trunk.
func (s *Server) buildTrunkRequest(st *trunkState, method, branch string, cseq, expires int, auth string) string {
	uri := st.trunkURI()
	user := st.user()
	domain := st.fromDomain()
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s SIP/2.0\r\n", method, uri)
	fmt.Fprintf(&b, "Via: SIP/2.0/UDP %s:%d;branch=%s\r\n", s.host, s.port, branch)
	fmt.Fprintf(&b, "From: <sip:%s@%s>;tag=%s\r\n", user, domain, st.fromTag)
	fmt.Fprintf(&b, "To: <sip:%s@%s>\r\n", user, domain)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", st.callID)
	fmt.Fprintf(&b, "CSeq: %d %s\r\n", cseq, method)
	fmt.Fprintf(&b, "Contact: <sip:%s@%s:%d>\r\n", user, s.host, s.port)
	if method == "REGISTER" {
		fmt.Fprintf(&b, "Expires: %d\r\n", expires)
	}
	if auth != "" {
		fmt.Fprintf(&b, "Authorization: %s\r\n", auth)
	}
	b.WriteString("Max-Forwards: 70\r\n")
	b.WriteString("Content-Length: 0\r\n\r\n")
	return b.String()
}

// trunkRequest sends one request and waits for its response, which is routed
// back by the server branch (GOBELISK-25).
func (s *Server) trunkRequest(st *trunkState, msg, branch string, timeout time.Duration) (*sip.RawMessage, error) {
	ch := make(chan *sip.RawMessage, 1)
	s.trunkWaitMu.Lock()
	s.trunkWait[branch] = ch
	s.trunkWaitMu.Unlock()
	defer func() {
		s.trunkWaitMu.Lock()
		delete(s.trunkWait, branch)
		s.trunkWaitMu.Unlock()
	}()

	addr := st.addr()
	if addr == nil {
		return nil, fmt.Errorf("cannot resolve trunk host %q", st.cfg.Host)
	}
	s.sendRaw(addr, msg)

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for %s response", methodOf(msg))
	case <-st.stop:
		return nil, fmt.Errorf("trunk stopped")
	}
}

func methodOf(msg string) string {
	if parts := strings.Fields(msg); len(parts) > 0 {
		return parts[0]
	}
	return "request"
}

// digestAuth turns a Digest challenge into an Authorization header value for
// the given request (GOBELISK-25).
func digestAuth(challenge, method, uri, username, password string) (string, error) {
	raw := strings.TrimSpace(challenge)
	chal, err := digest.ParseChallenge(raw)
	if err != nil {
		chal, err = digest.ParseChallenge(strings.TrimPrefix(raw, "Digest "))
		if err != nil {
			return "", fmt.Errorf("parse challenge: %w", err)
		}
	}
	cred, err := digest.Digest(chal, digest.Options{
		Method:   method,
		URI:      uri,
		Username: username,
		Password: password,
		Count:    1,
		Cnonce:   xid.New().String(),
	})
	if err != nil {
		return "", fmt.Errorf("digest: %w", err)
	}
	return cred.String(), nil
}

// registerTrunk performs one full register attempt: plain REGISTER, digest
// retry on 401/407, then success or failure bookkeeping.
func (s *Server) registerTrunk(st *trunkState) error {
	uri := st.trunkURI()
	cseq := st.cseq()

	branch := newBranch()
	msg := s.buildTrunkRequest(st, "REGISTER", branch, cseq, st.cfg.ExpiresOrDefault(), "")
	resp, err := s.trunkRequest(st, msg, branch, 5*time.Second)
	if err != nil {
		st.fail(err, "registration failed")
		return err
	}

	if resp.StatusCode == 401 || resp.StatusCode == 407 {
		challenge := getHeader(resp, "WWW-Authenticate")
		if challenge == "" {
			challenge = getHeader(resp, "Proxy-Authenticate")
		}
		auth, aerr := digestAuth(challenge, "REGISTER", uri, st.user(), st.cfg.Password)
		if aerr != nil {
			st.fail(aerr, "digest authentication failed")
			return aerr
		}
		branch = newBranch() // new transaction, same CSeq
		msg = s.buildTrunkRequest(st, "REGISTER", branch, cseq, st.cfg.ExpiresOrDefault(), auth)
		resp, err = s.trunkRequest(st, msg, branch, 5*time.Second)
		if err != nil {
			st.fail(err, "registration failed after challenge")
			return err
		}
	}

	if resp.StatusCode != 200 {
		e := fmt.Errorf("REGISTER: %d %s", resp.StatusCode, resp.StatusDesc)
		st.fail(e, "registration failed")
		return e
	}

	// The provider may grant a different lifetime than requested.
	expires := time.Duration(st.cfg.ExpiresOrDefault()) * time.Second
	if v := getHeader(resp, "Expires"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			expires = time.Duration(n) * time.Second
		}
	}
	st.registerOK(expires)
	s.log.Info().
		Str("trunk", st.cfg.Name).
		Str("type", st.cfg.TrunkType()).
		Dur("expires", expires).
		Msg("trunk registered")
	return nil
}

// trunkLoop keeps a trunk registered: renew at half the granted lifetime,
// retry failures after the configured delay (GOBELISK-25).
func (s *Server) trunkLoop(st *trunkState) {
	for {
		select {
		case <-st.stop:
			return
		default:
		}

		_ = s.registerTrunk(st)

		wait := st.renewIn()
		select {
		case <-st.stop:
			return
		case <-time.After(wait):
		}
	}
}

// trunkHealthLoop probes the trunk with OPTIONS while it is registered
// (GOBELISK-25: health checks run).
func (s *Server) trunkHealthLoop(st *trunkState) {
	for {
		select {
		case <-st.stop:
			return
		case <-time.After(st.cfg.HealthIntervalOrDefault()):
		}

		st.mu.Lock()
		registered := st.state == TrunkStateRegistered
		st.mu.Unlock()
		if !registered {
			continue
		}

		branch := newBranch()
		msg := s.buildTrunkRequest(st, "OPTIONS", branch, st.cseq(), 0, "")
		resp, err := s.trunkRequest(st, msg, branch, st.cfg.HealthTimeoutOrDefault())
		if err == nil && resp.StatusCode >= 300 {
			err = fmt.Errorf("OPTIONS: %d %s", resp.StatusCode, resp.StatusDesc)
		}
		if err != nil {
			st.fail(err, "health check failed")
			s.log.Warn().Str("trunk", st.cfg.Name).Err(err).Msg("trunk health check failed")
			continue
		}
		st.healthOK()
	}
}

// ---- lifecycle (config sync) ----

// syncTrunks reconciles the runtime trunk set with configuration: adds new
// trunks, restarts changed ones and stops removed ones.
func (s *Server) syncTrunks() {
	s.trunkMu.Lock()
	defer s.trunkMu.Unlock()
	s.syncTrunksLocked()
}

// syncTrunksLocked is syncTrunks with s.trunkMu already held.
func (s *Server) syncTrunksLocked() {
	seen := make(map[string]bool, len(s.cfg.Trunks))
	for _, tc := range s.cfg.Trunks {
		seen[tc.Name] = true
		st, ok := s.trunkStates[tc.Name]
		switch {
		case !ok:
			st = newTrunkState(tc)
			s.trunkStates[tc.Name] = st
			if s.listening {
				s.startTrunkLocked(st)
			}
		case !reflect.DeepEqual(st.cfg, tc):
			s.stopTrunkLocked(st)
			st = newTrunkState(tc)
			s.trunkStates[tc.Name] = st
			if s.listening {
				s.startTrunkLocked(st)
			}
		}
	}
	for name, st := range s.trunkStates {
		if !seen[name] {
			s.stopTrunkLocked(st)
			delete(s.trunkStates, name)
		}
	}
}

// startTrunkLoops starts every registering trunk's loops once the UDP socket
// is bound (called from ListenUDP).
func (s *Server) startTrunkLoops() {
	s.trunkMu.Lock()
	defer s.trunkMu.Unlock()
	s.listening = true
	for _, st := range s.trunkStates {
		s.startTrunkLocked(st)
	}
}

// startTrunkLocked spawns the loops of a trunk that should register.
// Callers hold s.trunkMu.
func (s *Server) startTrunkLocked(st *trunkState) {
	st.mu.Lock()
	shouldRun := st.state == TrunkStateRegistering
	already := st.started
	if shouldRun && !already {
		st.started = true
	}
	st.mu.Unlock()
	if shouldRun && !already {
		go s.trunkLoop(st)
		go s.trunkHealthLoop(st)
	}
}

// stopTrunkLocked stops a trunk's loops and best-effort deregisters it.
// Callers hold s.trunkMu.
func (s *Server) stopTrunkLocked(st *trunkState) {
	st.deregisterOnce.Do(func() {
		close(st.stop)
	})
	// Best-effort deregistration: tell the provider we are gone.
	if s.listening && st.cfg.RegisterEnabled() && st.cfg.TrunkType() != "iax" {
		if addr := st.addr(); addr != nil {
			branch := newBranch()
			msg := s.buildTrunkRequest(st, "REGISTER", branch, st.cseq(), 0, "")
			s.sendRaw(addr, msg)
		}
	}
}

// trunkStatus snapshots one trunk by name.
func (s *Server) trunkStatus(name string) (trunkStatus, bool) {
	s.trunkMu.Lock()
	defer s.trunkMu.Unlock()
	st, ok := s.trunkStates[name]
	if !ok {
		return trunkStatus{}, false
	}
	return st.status(), true
}

// routeTrunkResponse delivers a response whose top Via branch belongs to an
// outstanding trunk request. Returns false when the branch is unknown.
func (s *Server) routeTrunkResponse(resp *sip.RawMessage) bool {
	branch := topViaBranch(getHeader(resp, "Via"))
	if branch == "" {
		return false
	}
	s.trunkWaitMu.Lock()
	ch, ok := s.trunkWait[branch]
	s.trunkWaitMu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- resp:
	default:
	}
	return true
}

// ---- management RPC (GOBELISK-24) ----

func protoTrunk(tc config.TrunkConfig, status trunkStatus, known bool) *gobeliskv1.Trunk {
	t := &gobeliskv1.Trunk{
		Name:            tc.Name,
		Type:            tc.TrunkType(),
		Host:            tc.Host,
		Port:            int32(tc.PortOrDefault()),
		Transport:       tc.Transport,
		Username:        tc.Username,
		FromDomain:      tc.FromDomain,
		Callerid:        tc.CallerID,
		Enabled:         tc.RegisterEnabled(),
		RegisterExpires: int32(tc.ExpiresOrDefault()),
		Context:         tc.Context,
	}
	if known {
		t.State = status.State
		t.Alert = status.Alert
		t.LastError = status.LastError
		t.ConsecutiveFailures = int32(status.Failures)
		if !status.ExpiresAt.IsZero() {
			t.ExpiresUnix = status.ExpiresAt.Unix()
		}
		if !status.LastRegister.IsZero() {
			t.LastRegisterUnix = status.LastRegister.Unix()
		}
	}
	return t
}

// ListTrunks returns every provisioned trunk with its live status; the
// password is never included (GOBELISK-24 / GOBELISK-25).
func (s *Server) ListTrunks(ctx context.Context, req *gobeliskv1.ListTrunksRequest) (*gobeliskv1.ListTrunksResponse, error) {
	s.trunkMu.Lock()
	defer s.trunkMu.Unlock()
	out := make([]*gobeliskv1.Trunk, 0, len(s.cfg.Trunks))
	for _, tc := range s.cfg.Trunks {
		var status trunkStatus
		known := false
		if st, ok := s.trunkStates[tc.Name]; ok {
			status, known = st.status(), true
		}
		out = append(out, protoTrunk(tc, status, known))
	}
	return &gobeliskv1.ListTrunksResponse{Trunks: out}, nil
}

// CreateTrunk provisions a new trunk (runtime only: the config file remains
// the durable store).
func (s *Server) CreateTrunk(ctx context.Context, req *gobeliskv1.CreateTrunkRequest) (*gobeliskv1.CreateTrunkResponse, error) {
	if req.Trunk == nil {
		return nil, fmt.Errorf("trunk is required")
	}
	tc := trunkConfigFromProto(req.Trunk)
	if err := tc.Valid(); err != nil {
		return nil, err
	}
	s.trunkMu.Lock()
	defer s.trunkMu.Unlock()
	for _, existing := range s.cfg.Trunks {
		if existing.Name == tc.Name {
			return nil, fmt.Errorf("trunk %q already exists", tc.Name)
		}
	}
	s.cfg.Trunks = append(s.cfg.Trunks, tc)
	s.syncTrunksLocked()
	return &gobeliskv1.CreateTrunkResponse{Message: "ok"}, nil
}

// UpdateTrunk replaces a trunk's configuration. An empty password keeps the
// previous one, and the retry/health tuning (not carried by the RPC) is
// preserved.
func (s *Server) UpdateTrunk(ctx context.Context, req *gobeliskv1.UpdateTrunkRequest) (*gobeliskv1.UpdateTrunkResponse, error) {
	if req.Trunk == nil {
		return nil, fmt.Errorf("trunk is required")
	}
	tc := trunkConfigFromProto(req.Trunk)
	if err := tc.Valid(); err != nil {
		return nil, err
	}
	s.trunkMu.Lock()
	defer s.trunkMu.Unlock()
	for i, existing := range s.cfg.Trunks {
		if existing.Name != tc.Name {
			continue
		}
		if tc.Password == "" {
			tc.Password = existing.Password
		}
		tc.RetrySeconds = existing.RetrySeconds
		tc.HealthIntervalSeconds = existing.HealthIntervalSeconds
		tc.HealthTimeoutSeconds = existing.HealthTimeoutSeconds
		s.cfg.Trunks[i] = tc
		s.syncTrunksLocked()
		return &gobeliskv1.UpdateTrunkResponse{Message: "ok"}, nil
	}
	return nil, fmt.Errorf("trunk %q not found", tc.Name)
}

// RemoveTrunk deprovisions a trunk (stops its loops and deregisters it).
func (s *Server) RemoveTrunk(ctx context.Context, req *gobeliskv1.RemoveTrunkRequest) (*gobeliskv1.RemoveTrunkResponse, error) {
	s.trunkMu.Lock()
	defer s.trunkMu.Unlock()
	for i, existing := range s.cfg.Trunks {
		if existing.Name != req.Name {
			continue
		}
		s.cfg.Trunks = append(s.cfg.Trunks[:i], s.cfg.Trunks[i+1:]...)
		s.syncTrunksLocked()
		return &gobeliskv1.RemoveTrunkResponse{Message: "ok"}, nil
	}
	return nil, fmt.Errorf("trunk %q not found", req.Name)
}

func trunkConfigFromProto(t *gobeliskv1.Trunk) config.TrunkConfig {
	enabled := t.Enabled
	return config.TrunkConfig{
		Name:            strings.TrimSpace(t.Name),
		Type:            t.Type,
		Host:            strings.TrimSpace(t.Host),
		Port:            int(t.Port),
		Transport:       strings.ToLower(strings.TrimSpace(t.Transport)),
		Username:        t.Username,
		Password:        t.Password,
		FromDomain:      t.FromDomain,
		CallerID:        t.Callerid,
		Enabled:         &enabled,
		RegisterExpires: int(t.RegisterExpires),
		Context:         t.Context,
	}
}

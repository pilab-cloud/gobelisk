package pbx

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/devopshaven/gobelisk/gen/go/gobelisk/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"

	"github.com/devopshaven/gobelisk/pkg/config"
	"github.com/devopshaven/gobelisk/pkg/dialplan"
	"github.com/devopshaven/gobelisk/pkg/ivr"
	"gopkg.in/yaml.v3"

	"github.com/devopshaven/gobelisk/pkg/meeting"
	"github.com/devopshaven/gobelisk/pkg/ringgroup"
	"github.com/devopshaven/gobelisk/pkg/rooms"
	"github.com/devopshaven/gobelisk/pkg/sip"
	"github.com/devopshaven/gobelisk/pkg/sip/auth"
	"github.com/devopshaven/gobelisk/pkg/voicemail"
	"github.com/rs/zerolog"
)

// ReadBufferSize is the size of the read buffer.
const ReadBufferSize = 65535

// Server represents a SIP server.
type Server struct {
	gobeliskv1.UnimplementedManagementServer

	conn *net.UDPConn

	// server identity
	host string
	port int

	// pluggable modules
	auth          auth.Authenticator
	dialPlan      *dialplan.DialPlan
	rooms         *rooms.Manager
	ivr           *ivr.Manager
	meetingClient *meeting.Client

	// ringState tracks round-robin / least-recent scheduling for ring groups.
	ringState *ringgroup.State

	// configuration and RPC
	cfg        *config.Config
	configPath string
	rpcPort    int

	// server logger
	log zerolog.Logger

	// registered endpoints: extension -> registration
	regs  map[string]*Registration
	regMu sync.Mutex

	// active calls: call-id -> call
	calls  map[string]*Call
	callMu sync.Mutex

	// pending in-dialog transactions (GOBELISK-10): server branch -> tx and
	// dialog key -> branch, so responses are relayed to the right leg.
	pendingMu       sync.Mutex
	pendingByBranch map[string]*pendingTx
	pendingByKey    map[string]string

	// runtime Do Not Disturb state: configuration seeds it, feature codes
	// toggle it until the next reload (GOBELISK-18).
	dndMu sync.Mutex
	dndOn map[string]bool

	// park orbit (GOBELISK-12): slot -> parked call and slot -> expiry timer.
	parkMu     sync.Mutex
	parks      map[int]*parkEntry
	parkTimers map[int]*time.Timer

	// featureLast replays the answer to a retransmitted feature-code INVITE
	// instead of executing the action twice (GOBELISK-18).
	featureMu   sync.Mutex
	featureLast map[string]featureEntry

	// provisioned trunks and their registration/health loops (GOBELISK-24/25).
	trunkMu     sync.Mutex
	trunkStates map[string]*trunkState
	listening   bool
	// trunkWait routes responses of outbound trunk requests by branch.
	trunkWaitMu sync.Mutex
	trunkWait   map[string]chan *sip.RawMessage

	msgCh  chan sipMsg
	doneCh chan struct{}

	// onListen, when set, is called once the UDP socket is bound. Tests use it
	// to wait for the listener without racing on the server fields.
	onListen func()
}

type sipMsg struct {
	msg     *sip.RawMessage
	addr    *net.UDPAddr
	raw     []byte
	conn    net.Conn
	respond func(string)
}

// Registration holds a registered contact.
type Registration struct {
	Ext        string
	ContactURI string
	Addr       *net.UDPAddr
	Expires    time.Time
}

// Call holds state for an in-progress call.
type Call struct {
	CallID     string
	CallerAddr *net.UDPAddr
	CalleeExt  string
	CalleeAddr *net.UDPAddr
	MeetingID  string
	SessionID  string

	// Dialog identity (GOBELISK-10): tags and Contacts let in-dialog
	// requests be matched to the leg that sent them, not just to the Call-ID.
	CallerExt     string
	CallerTag     string // From-tag of the caller (UAC)
	CalleeTag     string // To-tag assigned by the callee (UAS)
	CallerContact string
	CalleeContact string

	// Answered is true once the callee accepted the call (2xx). It marks the
	// callee extension as busy for call-waiting purposes.
	Answered bool
	// Virtual calls are answered by the PBX itself (park slots): ACKs and
	// in-dialog requests never leave the server (GOBELISK-12).
	Virtual bool
	// Group is the ring group name while a group call is being set up.
	Group string

	mu       sync.Mutex
	branches map[string]*ringBranch // top Via branch -> forked INVITE target
	ringCh   chan ringBranchResp
	cancelCh chan struct{}

	// holdOffer is an unanswered hold/resume offer; heldBy is the leg that
	// currently holds the call (GOBELISK-10).
	holdOffer *holdOffer
	heldBy    string
}

// holdOffer is a hold/resume SDP offer that has been forwarded but not yet
// answered by the far leg.
type holdOffer struct {
	Leg string // "caller" or "callee"
	Dir string // sendonly / recvonly / inactive / sendrecv
}

// ringBranch is one forked INVITE in a ring group call.
type ringBranch struct {
	Ext  string
	Addr *net.UDPAddr
	Via  string
}

// ringBranchResp is a response received for one forked INVITE.
type ringBranchResp struct {
	branch *ringBranch
	resp   *sip.RawMessage
}

func (c *Call) addBranch(b *ringBranch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.branches == nil {
		c.branches = make(map[string]*ringBranch)
	}
	c.branches[b.Via] = b
}

func (c *Call) branchForVia(via string) *ringBranch {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.branches == nil {
		return nil
	}
	return c.branches[topViaBranch(via)]
}

func (c *Call) clearBranches() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.branches = nil
}

func (c *Call) branchesSnapshot() []*ringBranch {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*ringBranch, 0, len(c.branches))
	for _, b := range c.branches {
		out = append(out, b)
	}
	return out
}

// canceled reports whether the call was cancelled by the caller.
func (c *Call) canceled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancelCh == nil {
		return false
	}
	select {
	case <-c.cancelCh:
		return true
	default:
		return false
	}
}

// cancel signals ring group workers to stop trying members.
func (c *Call) cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancelCh != nil {
		select {
		case <-c.cancelCh:
		default:
			close(c.cancelCh)
		}
	}
}

// ServerOpts are options that can be passed to NewServer.
type ServerOpts = func(*Server)

// WithLogger sets the logger for the server.
func WithLogger(l zerolog.Logger) ServerOpts {
	return func(s *Server) {
		s.log = l
	}
}

// WithAuthenticator sets the authenticator module.
func WithAuthenticator(a auth.Authenticator) ServerOpts {
	return func(s *Server) {
		s.auth = a
	}
}

// WithDialPlan sets the dial plan module.
func WithDialPlan(d *dialplan.DialPlan) ServerOpts {
	return func(s *Server) {
		s.dialPlan = d
	}
}

// WithRooms sets the room manager module.
func WithRooms(r *rooms.Manager) ServerOpts {
	return func(s *Server) {
		s.rooms = r
	}
}

// WithIVR sets the IVR manager module.
func WithIVR(i *ivr.Manager) ServerOpts {
	return func(s *Server) {
		s.ivr = i
	}
}

func WithMeetingClient(c *meeting.Client) ServerOpts {
	return func(s *Server) {
		s.meetingClient = c
	}
}

// WithConfig sets the server configuration.
func WithConfig(c *config.Config) ServerOpts {
	return func(s *Server) {
		s.cfg = c
	}
}

// WithRPCPort sets the RPC listen port.
func WithRPCPort(p int) ServerOpts {
	return func(s *Server) {
		s.rpcPort = p
	}
}

// WithConfigPath sets the config file path for reload.
func WithConfigPath(p string) ServerOpts {
	return func(s *Server) {
		s.configPath = p
	}
}

// NewServer creates a new Server.
func NewServer(opts ...ServerOpts) *Server {
	s := &Server{
		host:            "127.0.0.1",
		regs:            make(map[string]*Registration),
		calls:           make(map[string]*Call),
		pendingByBranch: make(map[string]*pendingTx),
		pendingByKey:    make(map[string]string),
		dndOn:           make(map[string]bool),
		parks:           make(map[int]*parkEntry),
		parkTimers:      make(map[int]*time.Timer),
		featureLast:     make(map[string]featureEntry),
		trunkStates:     make(map[string]*trunkState),
		trunkWait:       make(map[string]chan *sip.RawMessage),
		ringState:       ringgroup.NewState(),
		msgCh:           make(chan sipMsg, 256),
		doneCh:          make(chan struct{}, 1),
		log:             zerolog.Nop(),
	}

	for _, o := range opts {
		o(s)
	}

	if s.auth == nil {
		s.auth = auth.NoneAuthenticator{}
	}
	if s.dialPlan == nil {
		s.dialPlan = dialplan.New()
	}
	if s.rooms == nil {
		s.rooms = rooms.NewManager()
	}
	if s.ivr == nil {
		s.ivr = ivr.NewManager()
	}
	if s.meetingClient == nil && s.cfg != nil && s.cfg.MeetingServiceURL != "" {
		s.meetingClient = meeting.New(s.cfg.MeetingServiceURL)
	}
	if s.cfg == nil {
		s.cfg = config.Default()
	}
	s.seedDND()
	s.syncTrunks()
	if s.rpcPort == 0 {
		s.rpcPort = 8080
	}

	return s
}

// Start starts the server's event loop.
func (s *Server) Start() {
	s.log.Info().Msg("SIP server starting")
}

// Close closes the server.
func (s *Server) Close() error {
	select {
	case s.doneCh <- struct{}{}:
	default:
	}
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// ListenUDP starts a UDP server on the specified port.
func (s *Server) ListenUDP(port int) error {
	s.port = port
	if ip := localIP(); ip != "" {
		s.host = ip
	}
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	s.conn = conn
	s.startTrunkLoops()

	s.log.Info().Msgf("server listening on :%d", port)

	if s.onListen != nil {
		s.onListen()
	}

	go s.readLoop(conn)

	// main loop
	for {
		select {
		case m := <-s.msgCh:
			if m.msg.Method != "" {
				go s.handleRequest(m)
			} else {
				s.routeResponse(m)
			}
		case <-s.doneCh:
			return nil
		}
	}
}

// ListenTCP starts a TCP SIP listener.
func (s *Server) ListenTCP(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	s.log.Info().Msgf("TCP server listening on :%d", port)
	for {
		conn, err := ln.Accept()
		if err != nil {
			s.log.Error().Err(err).Msg("tcp accept error")
			continue
		}
		go s.handleTCPConn(conn, false)
	}
}

// ListenTLS starts a TLS SIP listener.
func (s *Server) ListenTLS(port int) error {
	cert, err := generateSelfSignedCert()
	if err != nil {
		return err
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", port), cfg)
	if err != nil {
		return err
	}
	s.log.Info().Msgf("TLS server listening on :%d", port)
	for {
		conn, err := ln.Accept()
		if err != nil {
			s.log.Error().Err(err).Msg("tls accept error")
			continue
		}
		go s.handleTCPConn(conn, true)
	}
}

// StartRPC starts an HTTP JSON-RPC management server.
func (s *Server) StartRPC() error {
	// Unary interceptor for bearer token auth
	unaryInterceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if s.cfg != nil && s.cfg.Server.GRPCBearerToken != "" {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, fmt.Errorf("missing metadata")
			}
			auths := md.Get("authorization")
			if len(auths) == 0 || auths[0] != "Bearer "+s.cfg.Server.GRPCBearerToken {
				return nil, fmt.Errorf("unauthorized")
			}
		}
		return handler(ctx, req)
	}

	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(unaryInterceptor),
	}

	// mTLS / TLS
	if s.cfg != nil && s.cfg.Server.GRPCTLSCert != "" && s.cfg.Server.GRPCTLSKey != "" {
		cert, err := tls.LoadX509KeyPair(s.cfg.Server.GRPCTLSCert, s.cfg.Server.GRPCTLSKey)
		if err != nil {
			return err
		}
		tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
		if s.cfg.Server.GRPCCACert != "" {
			ca, err := os.ReadFile(s.cfg.Server.GRPCCACert)
			if err != nil {
				return err
			}
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(ca)
			tlsCfg.ClientCAs = pool
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		}
		creds := credentials.NewTLS(tlsCfg)
		opts = append(opts, grpc.Creds(creds))
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.rpcPort))
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer(opts...)
	gobeliskv1.RegisterManagementServer(grpcServer, s)
	reflection.Register(grpcServer)
	s.log.Info().Msgf("gRPC server listening on :%d", s.rpcPort)
	return grpcServer.Serve(lis)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      interface{}     `json:"id"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}

	result, err := s.dispatchRPC(req.Method, req.Params)
	if err != nil {
		writeRPCError(w, req.ID, -32000, err.Error())
		return
	}
	resp := rpcResponse{JSONRPC: "2.0", Result: result, ID: req.ID}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func writeRPCError(w http.ResponseWriter, id interface{}, code int, msg string) {
	resp := rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: code, Message: msg}, ID: id}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) dispatchRPC(method string, params json.RawMessage) (interface{}, error) {
	switch method {
	case "status":
		return map[string]interface{}{
			"host": s.host,
			"port": s.port,
		}, nil
	case "config.get":
		return s.cfg, nil
	case "config.set":
		var c config.Config
		if err := json.Unmarshal(params, &c); err != nil {
			return nil, err
		}
		s.cfg = &c
		if s.dialPlan != nil && c.DialPlan != "" {
			ivr.NewManager()
			dialplan.ApplyConfig(s.dialPlan, s.ivr, c.DialPlan)
		}
		return "ok", nil
	case "config.reload":
		c, err := config.Load(s.cfgFile())
		if err != nil {
			return nil, err
		}
		s.cfg = c
		return "ok", nil
	case "users.list":
		return s.cfg.Auth.Users, nil
	case "users.add":
		var p struct {
			User     string `json:"user"`
			Password string `json:"password"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		if s.cfg.Auth.Users == nil {
			s.cfg.Auth.Users = map[string]string{}
		}
		s.cfg.Auth.Users[p.User] = p.Password
		return "ok", nil
	case "users.remove":
		var p struct {
			User string `json:"user"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		delete(s.cfg.Auth.Users, p.User)
		return "ok", nil
	case "dialplan.get":
		return s.cfg.DialPlan, nil
	case "dialplan.set":
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		s.cfg.DialPlan = p.Text
		if s.dialPlan != nil {
			dialplan.ApplyConfig(s.dialPlan, s.ivr, p.Text)
		}
		return "ok", nil
	case "calls.list":
		s.callMu.Lock()
		defer s.callMu.Unlock()
		ids := make([]string, 0, len(s.calls))
		for id := range s.calls {
			ids = append(ids, id)
		}
		return ids, nil
	case "rooms.list":
		return "not implemented", nil
	default:
		return nil, fmt.Errorf("unknown method: %s", method)
	}
}

func (s *Server) mustEmbedUnimplementedManagementServer() {}

func (s *Server) cfgFile() string {
	return s.configPath
}

// gRPC Management implementation
func (s *Server) Status(ctx context.Context, req *gobeliskv1.StatusRequest) (*gobeliskv1.StatusResponse, error) {
	return &gobeliskv1.StatusResponse{Host: s.host, Port: int32(s.port)}, nil
}

func (s *Server) GetConfig(ctx context.Context, req *gobeliskv1.GetConfigRequest) (*gobeliskv1.GetConfigResponse, error) {
	data, err := yaml.Marshal(s.cfg)
	if err != nil {
		return nil, err
	}
	return &gobeliskv1.GetConfigResponse{Yaml: string(data)}, nil
}

func (s *Server) SetConfig(ctx context.Context, req *gobeliskv1.SetConfigRequest) (*gobeliskv1.SetConfigResponse, error) {
	var c config.Config
	if err := yaml.Unmarshal([]byte(req.Yaml), &c); err != nil {
		return nil, err
	}
	s.cfg = &c
	s.seedDND()
	s.syncTrunks()
	if s.dialPlan != nil && c.DialPlan != "" {
		dialplan.ApplyConfig(s.dialPlan, s.ivr, c.DialPlan)
	}
	return &gobeliskv1.SetConfigResponse{Message: "ok"}, nil
}

func (s *Server) ReloadConfig(ctx context.Context, req *gobeliskv1.ReloadConfigRequest) (*gobeliskv1.ReloadConfigResponse, error) {
	c, err := config.Load(s.configPath)
	if err != nil {
		return nil, err
	}
	s.cfg = c
	s.seedDND()
	s.syncTrunks()
	if s.dialPlan != nil && c.DialPlan != "" {
		dialplan.ApplyConfig(s.dialPlan, s.ivr, c.DialPlan)
	}
	return &gobeliskv1.ReloadConfigResponse{Message: "ok"}, nil
}

func (s *Server) ListUsers(ctx context.Context, req *gobeliskv1.ListUsersRequest) (*gobeliskv1.ListUsersResponse, error) {
	if s.cfg.Auth.Users == nil {
		s.cfg.Auth.Users = map[string]string{}
	}
	return &gobeliskv1.ListUsersResponse{Users: s.cfg.Auth.Users}, nil
}

func (s *Server) AddUser(ctx context.Context, req *gobeliskv1.AddUserRequest) (*gobeliskv1.AddUserResponse, error) {
	if s.cfg.Auth.Users == nil {
		s.cfg.Auth.Users = map[string]string{}
	}
	s.cfg.Auth.Users[req.User] = req.Password
	return &gobeliskv1.AddUserResponse{Message: "ok"}, nil
}

func (s *Server) RemoveUser(ctx context.Context, req *gobeliskv1.RemoveUserRequest) (*gobeliskv1.RemoveUserResponse, error) {
	delete(s.cfg.Auth.Users, req.User)
	return &gobeliskv1.RemoveUserResponse{Message: "ok"}, nil
}

func (s *Server) GetDialPlan(ctx context.Context, req *gobeliskv1.GetDialPlanRequest) (*gobeliskv1.GetDialPlanResponse, error) {
	return &gobeliskv1.GetDialPlanResponse{Text: s.cfg.DialPlan}, nil
}

func (s *Server) SetDialPlan(ctx context.Context, req *gobeliskv1.SetDialPlanRequest) (*gobeliskv1.SetDialPlanResponse, error) {
	s.cfg.DialPlan = req.Text
	if s.dialPlan != nil {
		dialplan.ApplyConfig(s.dialPlan, s.ivr, req.Text)
	}
	return &gobeliskv1.SetDialPlanResponse{Message: "ok"}, nil
}

func (s *Server) RemoveExtension(ctx context.Context, req *gobeliskv1.RemoveExtensionRequest) (*gobeliskv1.RemoveExtensionResponse, error) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	delete(s.regs, req.Extension)
	return &gobeliskv1.RemoveExtensionResponse{Message: "ok"}, nil
}

func (s *Server) ListRooms(ctx context.Context, req *gobeliskv1.ListRoomsRequest) (*gobeliskv1.ListRoomsResponse, error) {
	return &gobeliskv1.ListRoomsResponse{Rooms: s.rooms.List()}, nil
}

func (s *Server) CreateRoom(ctx context.Context, req *gobeliskv1.CreateRoomRequest) (*gobeliskv1.CreateRoomResponse, error) {
	s.rooms.Join(req.Name, "system")
	return &gobeliskv1.CreateRoomResponse{Message: "ok"}, nil
}

func (s *Server) RemoveRoom(ctx context.Context, req *gobeliskv1.RemoveRoomRequest) (*gobeliskv1.RemoveRoomResponse, error) {
	// no delete method; leave as no-op
	return &gobeliskv1.RemoveRoomResponse{Message: "ok"}, nil
}

func (s *Server) ListMeetings(ctx context.Context, req *gobeliskv1.ListMeetingsRequest) (*gobeliskv1.ListMeetingsResponse, error) {
	out := make(map[string]string)
	for k, v := range s.dialPlan.Meetings {
		out[k] = v
	}
	return &gobeliskv1.ListMeetingsResponse{Meetings: out}, nil
}

func (s *Server) CreateMeeting(ctx context.Context, req *gobeliskv1.CreateMeetingRequest) (*gobeliskv1.CreateMeetingResponse, error) {
	s.dialPlan.Meetings[req.Name] = req.Extension
	return &gobeliskv1.CreateMeetingResponse{Message: "ok"}, nil
}

func (s *Server) RemoveMeeting(ctx context.Context, req *gobeliskv1.RemoveMeetingRequest) (*gobeliskv1.RemoveMeetingResponse, error) {
	delete(s.dialPlan.Meetings, req.Name)
	return &gobeliskv1.RemoveMeetingResponse{Message: "ok"}, nil
}

func (s *Server) GetExtension(ctx context.Context, req *gobeliskv1.GetExtensionRequest) (*gobeliskv1.GetExtensionResponse, error) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	reg, ok := s.regs[req.Extension]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return &gobeliskv1.GetExtensionResponse{Contact: reg.ContactURI}, nil
}

func (s *Server) CreateExtension(ctx context.Context, req *gobeliskv1.CreateExtensionRequest) (*gobeliskv1.CreateExtensionResponse, error) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	s.regs[req.Extension] = &Registration{Ext: req.Extension, ContactURI: req.Contact, Addr: nil, Expires: time.Now().Add(365 * 24 * time.Hour)}
	return &gobeliskv1.CreateExtensionResponse{Message: "ok"}, nil
}

func (s *Server) UpdateExtension(ctx context.Context, req *gobeliskv1.UpdateExtensionRequest) (*gobeliskv1.UpdateExtensionResponse, error) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	if reg, ok := s.regs[req.Extension]; ok {
		reg.ContactURI = req.Contact
	} else {
		s.regs[req.Extension] = &Registration{Ext: req.Extension, ContactURI: req.Contact, Expires: time.Now().Add(365 * 24 * time.Hour)}
	}
	return &gobeliskv1.UpdateExtensionResponse{Message: "ok"}, nil
}

func (s *Server) ListExtensions(ctx context.Context, req *gobeliskv1.ListExtensionsRequest) (*gobeliskv1.ListExtensionsResponse, error) {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	out := make(map[string]string)
	for ext, reg := range s.regs {
		out[ext] = reg.ContactURI
	}
	return &gobeliskv1.ListExtensionsResponse{Extensions: out}, nil
}

func (s *Server) OriginateCall(ctx context.Context, req *gobeliskv1.OriginateCallRequest) (*gobeliskv1.OriginateCallResponse, error) {
	reg := s.getReg(req.To)
	if reg == nil {
		return nil, fmt.Errorf("callee not registered")
	}
	// build a minimal INVITE
	body := "v=0\r\no=server 1 1 IN IP4 " + s.host + "\r\ns=-\r\nc=IN IP4 " + s.host + "\r\nt=0 0\r\nm=audio 5004 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n"
	msg := fmt.Sprintf("INVITE sip:%s SIP/2.0\r\nVia: SIP/2.0/UDP %s:%d;branch=z9hG4bK%d\r\nFrom: <sip:%s@%s>;tag=server\r\nTo: <sip:%s@%s>\r\nCall-ID: %d@%s\r\nCSeq: 1 INVITE\r\nContact: <sip:server@%s:%d>\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s",
		req.To, s.host, s.port, time.Now().UnixNano(), req.From, s.host, req.To, s.host, time.Now().UnixNano(), s.host, s.host, s.port, len(body), body)
	s.sendRaw(reg.Addr, msg)
	return &gobeliskv1.OriginateCallResponse{Message: "ok"}, nil
}

func (s *Server) GeneratePrompt(ctx context.Context, req *gobeliskv1.GeneratePromptRequest) (*gobeliskv1.GeneratePromptResponse, error) {
	voice := req.Voice
	cmd := ""
	modelPath := ""
	if s.cfg != nil {
		if voice == "" {
			voice = s.cfg.VoiceAgent.TTS.Voice
		}
		cmd = s.cfg.VoiceAgent.TTS.Command
		modelPath = s.cfg.VoiceAgent.TTS.ModelPath
	}
	if voice == "" {
		voice = "anna"
	}
	out := req.OutputPath
	if out == "" {
		out = "/tmp/prompt.wav"
	}
	if err := voicemail.TTSWithConfig(req.Text, out, cmd, modelPath, voice); err != nil {
		return nil, err
	}
	return &gobeliskv1.GeneratePromptResponse{Path: out}, nil
}

func (s *Server) TranscribeAudio(ctx context.Context, req *gobeliskv1.TranscribeAudioRequest) (*gobeliskv1.TranscribeAudioResponse, error) {
	// Convert to 16kHz if needed
	tmp := req.AudioPath + ".16k.wav"
	if err := exec.Command("ffmpeg", "-y", "-i", req.AudioPath, "-ar", "16000", "-ac", "1", tmp).Run(); err != nil {
		s.log.Warn().Err(err).Msg("ffmpeg conversion failed")
		tmp = req.AudioPath
	}
	var modelPath, device, language string
	if s.cfg != nil {
		modelPath = s.cfg.VoiceAgent.ASR.ModelPath
		device = s.cfg.VoiceAgent.ASR.Device
		language = s.cfg.VoiceAgent.ASR.Language
	}
	text, err := voicemail.TranscribeWithConfig(tmp, modelPath, device, language)
	if err != nil {
		s.log.Warn().Err(err).Str("tmp", tmp).Msg("transcription failed")
	}
	if err != nil {
		return nil, err
	}
	return &gobeliskv1.TranscribeAudioResponse{Text: text}, nil
}

func (s *Server) ListCalls(ctx context.Context, req *gobeliskv1.ListCallsRequest) (*gobeliskv1.ListCallsResponse, error) {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	ids := make([]string, 0, len(s.calls))
	for id := range s.calls {
		ids = append(ids, id)
	}
	return &gobeliskv1.ListCallsResponse{CallIds: ids}, nil
}

func (s *Server) handleTCPConn(conn net.Conn, tlsMode bool) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		raw, err := readSIPMessageFromReader(reader)
		if err != nil {
			return
		}
		var rm sip.RawMessage
		if err := sip.UnmarshalBytes(&rm, raw); err != nil {
			s.log.Error().Err(err).Msg("failed to unmarshal SIP message")
			continue
		}
		addr := &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0}
		if ra, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
			addr = &net.UDPAddr{IP: ra.IP, Port: ra.Port}
		}
		m := sipMsg{msg: &rm, addr: addr, raw: raw, conn: conn, respond: func(msg string) {
			conn.Write([]byte(msg))
		}}
		if rm.Method != "" {
			s.handleRequest(m)
		} else {
			s.routeResponse(m)
		}
	}
}

func readSIPMessageFromReader(r *bufio.Reader) ([]byte, error) {
	// Read headers until blank line
	var headerBytes []byte
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		headerBytes = append(headerBytes, line...)
		if string(line) == "\r\n" || string(line) == "\n" {
			break
		}
	}
	// Parse Content-Length
	cl := 0
	for _, line := range strings.Split(string(headerBytes), "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "content-length:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &cl)
			}
		}
	}
	body := make([]byte, cl)
	if cl > 0 {
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, err
		}
	}
	return append(headerBytes, body...), nil
}

func generateSelfSignedCert() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "gobelisk"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	return tls.X509KeyPair(certPEM, keyPEM)
}

func (s *Server) readLoop(conn *net.UDPConn) {
	buf := make([]byte, ReadBufferSize)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			s.log.Error().Err(err).Msg("udp read error")
			continue
		}
		b := append([]byte{}, buf[:n]...)
		var rm sip.RawMessage
		if err := sip.UnmarshalBytes(&rm, b); err != nil {
			s.log.Error().Err(err).Msg("failed to unmarshal SIP message")
			continue
		}
		localConn := s.conn
		s.msgCh <- sipMsg{msg: &rm, addr: addr, raw: b, respond: func(m string) {
			if localConn != nil {
				localConn.WriteToUDP([]byte(m), addr)
			}
		}}
	}
}

// ---- request handling ----

func (s *Server) handleRequest(m sipMsg) {
	req := m.msg
	switch req.Method {
	case "REGISTER":
		s.handleRegister(m)
	case "INVITE":
		s.handleInvite(m)
	case "ACK":
		s.handleAck(m)
	case "BYE":
		s.handleBye(m)
	case "CANCEL":
		s.handleCancel(m)
	case "OPTIONS":
		s.handleOptions(m)
	case "PRACK":
		m.respond(buildResponse(req, 200, "OK", nil, ""))
	case "UPDATE":
		s.handleInDialogUpdate(m)
	case "INFO":
		s.handleInfo(m)
	case "MESSAGE":
		s.handleMessaging(m)
	case "SUBSCRIBE":
		m.respond(buildResponse(req, 202, "Accepted", nil, ""))
	case "NOTIFY":
		s.handleNotify(m)
	case "REFER":
		s.handleRefer(m)
	default:
		m.respond(buildResponse(req, 405, "Method Not Allowed", map[string]string{
			"Allow": "INVITE, ACK, OPTIONS, BYE, CANCEL, REGISTER, PRACK, UPDATE, INFO, MESSAGE, SUBSCRIBE, NOTIFY, REFER",
		}, ""))
	}
}

func (s *Server) handleRegister(m sipMsg) {
	req := m.msg

	// Pluggable authentication
	authHeader := getHeader(req, "Authorization")
	if _, isNone := s.auth.(auth.NoneAuthenticator); !isNone {
		if authHeader == "" {
			extra := map[string]string{}
			if h := s.auth.AuthHeader(); h != "" {
				extra["WWW-Authenticate"] = h
			}
			m.respond(buildResponse(req, 401, "Unauthorized", extra, ""))
			return
		}
		if err := s.auth.Authenticate(authHeader); err != nil {
			s.log.Warn().Err(err).Msg("registration auth failed")
			m.respond(buildResponse(req, 403, "Forbidden", nil, ""))
			return
		}
	}

	ext := userFromURI(getHeader(req, "To"))
	if ext == "" {
		ext = userFromURI(req.Target)
	}

	contact := getHeader(req, "Contact")
	caddr := parseContactAddr(contact)
	if caddr == nil {
		caddr = m.addr
	}

	s.regMu.Lock()
	s.regs[ext] = &Registration{
		Ext:        ext,
		ContactURI: contact,
		Addr:       caddr,
		Expires:    time.Now().Add(3600 * time.Second),
	}
	s.regMu.Unlock()

	s.log.Info().Str("ext", ext).Str("contact", contact).Msg("registered")

	extra := map[string]string{}
	if contact != "" {
		extra["Contact"] = contact
	}
	extra["Expires"] = "3600"
	m.respond(buildResponse(req, 200, "OK", extra, ""))
}

func (s *Server) handleMessaging(m sipMsg) {
	req := m.msg

	toExt := userFromURI(req.Target)
	if toExt == "" {
		toExt = userFromURI(getHeader(req, "To"))
	}

	reg := s.getReg(toExt)
	if reg == nil {
		m.respond(buildResponse(req, 404, "Not Found", nil, ""))
		return
	}

	// Acknowledge to sender
	m.respond(buildResponse(req, 200, "OK", nil, ""))

	// Forward MESSAGE to recipient
	branch := fmt.Sprintf("z9hG4bK%d", time.Now().UnixNano())
	serverVia := fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=%s", s.host, s.port, branch)
	msg := buildRequest("MESSAGE", req.Target, req, map[string]string{
		"Via": serverVia,
	}, req.Body)
	s.sendRaw(reg.Addr, msg)
}

func (s *Server) handleInfo(m sipMsg) {
	req := m.msg
	callID := getHeader(req, "Call-ID")

	// Parse DTMF from body: "Signal=1\r\nDuration=100"
	signal := ""
	for _, line := range strings.Split(req.Body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "signal=") {
			signal = strings.TrimSpace(line[len("signal="):])
		}
	}
	if signal == "" {
		// maybe body is just the digit
		signal = strings.TrimSpace(req.Body)
	}

	m.respond(buildResponse(req, 200, "OK", nil, ""))

	if signal == "" {
		return
	}

	complete, pin := s.ivr.HandleDigit(callID, signal)
	s.log.Info().Str("callID", callID).Str("digit", signal).Str("pin", pin).Bool("complete", complete).Msg("DTMF received")

	if complete {
		sess, ok := s.ivr.GetSession(callID)
		if ok && sess != nil {
			if sess.ValidPIN == "" || pin == sess.ValidPIN {
				s.log.Info().Str("callID", callID).Str("meeting", sess.Meeting).Str("pin", pin).Msg("PIN ok, resolving meeting")
				if s.meetingClient != nil {
					resolved, err := s.meetingClient.ResolveCallableMeeting(pin)
					if err != nil {
						s.log.Warn().Err(err).Str("callID", callID).Msg("meeting resolution failed")
					} else if resolved != nil {
						sess.MeetingID = resolved.MeetingID
						sess.SessionID = resolved.MeetingSessionID
						s.log.Info().Str("callID", callID).Str("meetingID", resolved.MeetingID).Str("sessionID", resolved.MeetingSessionID).Msg("meeting resolved")
					}
				}
			} else {
				s.log.Warn().Str("callID", callID).Msg("invalid PIN")
			}
			s.ivr.EndSession(callID)
		}
	}
}

func (s *Server) handleOptions(m sipMsg) {
	req := m.msg
	extra := map[string]string{
		"Allow": "INVITE, ACK, OPTIONS, BYE, CANCEL, REGISTER, PRACK, UPDATE, INFO, MESSAGE, SUBSCRIBE, NOTIFY, REFER",
	}
	m.respond(buildResponse(req, 200, "OK", extra, ""))
}

func (s *Server) handleInvite(m sipMsg) {
	req := m.msg
	callerAddr := m.addr

	callID := getHeader(req, "Call-ID")

	// In-dialog re-INVITE (hold/resume, codec change, session refresh):
	// relay it to the opposite leg with its response path (GOBELISK-10).
	if existing := s.getCall(callID); existing != nil {
		if inDialog(req) {
			s.forwardInDialog(m, existing, "INVITE")
			return
		}
		// The original out-of-dialog INVITE retransmitted: absorb it through
		// its pending transaction instead of routing the call a second time.
		key := pendingKey(callID, "caller", cseqNumber(req), "INVITE")
		if tx := s.lookupPendingKey(key); tx != nil {
			if tx.lastResp != nil {
				m.respond(s.relayedResponse(tx.lastResp, tx.orig))
			} else {
				m.respond(buildResponse(req, 100, "Trying", nil, ""))
			}
			return
		}
		// No transaction (mid no-answer switch or expired): still never
		// route the same Call-ID twice.
		m.respond(buildResponse(req, 100, "Trying", nil, ""))
		return
	} else if inDialog(req) {
		// An in-dialog request for a call we do not know: wrong dialog.
		m.respond(buildResponse(req, 481, "Call/Transaction Does Not Exist", nil, ""))
		return
	}

	dialed := userFromURI(req.Target)
	if dialed == "" {
		dialed = userFromURI(getHeader(req, "To"))
	}

	// Feature codes (GOBELISK-18): *78/*79 toggle DND, *70 parks the caller
	// by routing it into the next free orbit slot.
	if intercepted, route := s.featureCode(m, dialed); intercepted {
		if route == "" {
			return
		}
		dialed = route
	}

	fromHeader := getHeader(req, "From")
	overall := time.After(60 * time.Second)
	hops := 0

	for {
		// Route through dialplan + DND + call forwarding; resolveCallee
		// sends the rejection responses itself (GOBELISK-14 / GOBELISK-15).
		res, ok := s.resolveCallee(m, dialed, &hops)
		if !ok {
			return
		}
		if res.ringGroup != "" {
			s.handleRingGroupInvite(m, res.ringGroup)
			return
		}
		if res.parkEmpty {
			// The server answers here and keeps the caller parked
			// (GOBELISK-12).
			s.answerParked(m, callID, res.parkSlot)
			return
		}

		// 100 Trying (one per attempt, like a proxy forking serially)
		m.respond(buildResponse(req, 100, "Trying", nil, ""))

		// The first attempt creates the call; a no-answer forward retargets
		// the existing record so the caller's dialog stays intact.
		call := s.getCall(callID)
		if call == nil {
			call = &Call{
				CallID:        callID,
				CallerAddr:    callerAddr,
				CalleeExt:     res.calleeExt,
				CalleeAddr:    res.reg.Addr,
				CallerExt:     userFromURI(fromHeader),
				CallerTag:     tagOf(fromHeader),
				CallerContact: getHeader(req, "Contact"),
			}
			s.callMu.Lock()
			s.calls[callID] = call
			s.callMu.Unlock()
		} else {
			call.CalleeExt = res.calleeExt
			call.CalleeAddr = res.reg.Addr
		}

		branch := newBranch()
		serverVia := fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=%s", s.host, s.port, branch)

		reqURI := fmt.Sprintf("sip:%s@%s", res.calleeExt, res.reg.Addr.String())
		extraHeaders := map[string]string{
			"Via":     serverVia,
			"Contact": s.serverContact(),
		}
		// RFC 3891: Replaces may arrive as a URI parameter; convert it into a
		// header for the next hop (GOBELISK-11).
		if v := replacesParam(req.Target); v != "" && getHeader(req, "Replaces") == "" {
			extraHeaders["Replaces"] = v
		}
		if res.callWaiting {
			// RFC 7462 call-waiting alert so the phone plays the special tone.
			extraHeaders["Alert-Info"] = "<urn:alert:service:call-waiting>"
		}

		inviteMsg := buildRequest("INVITE", reqURI, req, extraHeaders, req.Body)

		// Register the transaction before sending so the callee's responses are
		// relayed back to the caller with the right Via path (GOBELISK-10).
		notify := make(chan *sip.RawMessage, 64)
		s.registerPending(branch, &pendingTx{
			key:     pendingKey(callID, "caller", cseqNumber(req), "INVITE"),
			call:    call,
			leg:     "caller",
			orig:    req,
			respond: m.respond,
			notify:  notify,
		})

		s.log.Info().Str("callID", callID).Str("callee", res.calleeExt).Msg("forwarding INVITE")
		s.log.Debug().Msg(inviteMsg)
		s.sendRaw(res.reg.Addr, inviteMsg)

		// Wait for the callee: an answer ends the setup, a configured
		// no-answer timeout cancels this leg and rings the forward target.
		var noAnswer <-chan time.Time
		if d := res.fwd.NoAnswerTimer(); d > 0 {
			noAnswer = time.After(d)
		}
		legTimer := time.After(legTimeout)

		select {
		case resp := <-notify:
			if resp.StatusCode >= 200 {
				// A successful pickup of a parked call releases the
				// parked dialog (GOBELISK-12).
				if resp.StatusCode < 300 && res.parked && !res.parkEmpty {
					s.releasePark(res.parkSlot, "picked up")
				}
				if resp.StatusCode >= 300 {
					s.removeCall(callID)
				}
				return
			}
		case <-noAnswer:
			// An answer racing the timer wins (GOBELISK-15).
			select {
			case resp := <-notify:
				if resp.StatusCode >= 200 {
					if resp.StatusCode >= 300 {
						s.removeCall(callID)
					}
					return
				}
			default:
			}
			s.log.Info().
				Str("callID", callID).
				Str("callee", res.calleeExt).
				Str("to", res.fwd.NoAnswer).
				Msg("no answer, forwarding")
			s.cancelForwardedInvite(reqURI, res.reg.Addr, branch, req)
			s.removePending(branch)
			dialed = res.fwd.NoAnswer
			hops++
		case <-legTimer:
			m.respond(buildResponse(req, 408, "Request Timeout", nil, ""))
			s.removeCall(callID)
			s.removePending(branch)
			return
		case <-overall:
			m.respond(buildResponse(req, 408, "Request Timeout", nil, ""))
			s.removeCall(callID)
			s.removePending(branch)
			return
		}
	}
}

// handleRingGroupInvite forks an incoming INVITE to the available members of a
// ring group and bridges the caller to the first member that answers.
func (s *Server) handleRingGroupInvite(m sipMsg, name string) {
	req := m.msg
	callID := getHeader(req, "Call-ID")

	group := s.dialPlan.RingGroups[name]
	if group == nil {
		s.log.Warn().Str("group", name).Msg("unknown ring group")
		m.respond(buildResponse(req, 404, "Not Found", nil, ""))
		return
	}

	// Only registered members that are not already in a call and without DND
	// can ring (GOBELISK-14).
	var members []*Registration
	for _, ext := range group.Members {
		if s.dndEnabled(ext) {
			s.log.Debug().Str("ext", ext).Str("group", name).Msg("skipping DND ring group member")
			continue
		}
		if s.extBusy(ext) {
			s.log.Debug().Str("ext", ext).Str("group", name).Msg("skipping busy ring group member")
			continue
		}
		if reg := s.getReg(ext); reg != nil && reg.Addr != nil {
			members = append(members, reg)
		}
	}
	if len(members) == 0 {
		s.log.Info().Str("group", name).Msg("ring group has no available members")
		m.respond(buildResponse(req, 480, "Temporarily Unavailable", nil, ""))
		return
	}

	order := s.ringState.Order(name, group.Strategy, registrationExts(members))
	byExt := make(map[string]*Registration, len(members))
	for _, reg := range members {
		byExt[reg.Ext] = reg
	}
	ordered := make([]*Registration, 0, len(order))
	for _, ext := range order {
		if reg := byExt[ext]; reg != nil {
			ordered = append(ordered, reg)
		}
	}

	s.log.Info().Str("callID", callID).Str("group", name).Str("strategy", string(group.Strategy)).Int("members", len(ordered)).Msg("ring group call")

	m.respond(buildResponse(req, 100, "Trying", nil, ""))

	call := &Call{
		CallID:        callID,
		CallerAddr:    m.addr,
		Group:         name,
		CallerExt:     userFromURI(getHeader(req, "From")),
		CallerTag:     tagOf(getHeader(req, "From")),
		CallerContact: getHeader(req, "Contact"),
		ringCh:        make(chan ringBranchResp, 256),
		cancelCh:      make(chan struct{}),
	}
	s.callMu.Lock()
	s.calls[callID] = call
	s.callMu.Unlock()

	answered := false
	defer func() {
		if !answered {
			s.removeCall(callID)
		}
	}()

	var winner *ringBranch
	var winnerResp *sip.RawMessage
	switch group.Strategy {
	case dialplan.StrategyRoundRobin, dialplan.StrategyLeastRecent:
		winner, winnerResp = s.ringSequential(m, call, ordered)
	default:
		winner, winnerResp = s.ringSimultaneous(m, call, ordered)
	}

	if winner == nil {
		s.cancelRingBranches(req, call, nil)
		if call.canceled() {
			// The caller hung up; the CANCEL exchange already answered it.
			return
		}
		s.log.Info().Str("callID", callID).Str("group", name).Msg("ring group call went unanswered")
		m.respond(buildResponse(req, 480, "Temporarily Unavailable", nil, ""))
		return
	}

	// Everyone else stops ringing; the caller is bridged to the winner.
	s.cancelRingBranches(req, call, winner)
	call.clearBranches()

	answered = true
	s.bridgeRingWinner(call, winner)
	s.ringState.MarkAnswered(name, winner.Ext)

	s.log.Info().Str("callID", callID).Str("group", name).Str("callee", winner.Ext).Msg("ring group answered")
	call.setCalleeIdentity(getHeader(winnerResp, "To"), getHeader(winnerResp, "Contact"))
	m.respond(s.relayedResponse(winnerResp, req))
}

// ringSimultaneous rings all members at once and returns the first to answer.
func (s *Server) ringSimultaneous(m sipMsg, call *Call, members []*Registration) (*ringBranch, *sip.RawMessage) {
	req := m.msg

	pending := 0
	for _, reg := range members {
		s.sendRingInvite(req, call, reg)
		pending++
	}

	timeout := time.After(s.ringTimeout())
	ringing := false
	for pending > 0 {
		select {
		case r := <-call.ringCh:
			switch code := r.resp.StatusCode; {
			case code < 180:
				// 100 Trying, keep waiting.
			case code == 180 || code == 183:
				if !ringing {
					m.respond(buildResponse(req, 180, "Ringing", nil, ""))
					ringing = true
				}
			case code >= 200 && code < 300:
				return r.branch, r.resp
			case code >= 300:
				pending--
			}
		case <-call.cancelCh:
			return nil, nil
		case <-timeout:
			return nil, nil
		}
	}
	return nil, nil
}

// ringSequential rings one member at a time, in the order given by the
// strategy, and moves on when a member does not answer in time or rejects the
// call.
func (s *Server) ringSequential(m sipMsg, call *Call, members []*Registration) (*ringBranch, *sip.RawMessage) {
	req := m.msg
	ringing := false

	for _, reg := range members {
		br := s.sendRingInvite(req, call, reg)
		deadline := time.After(s.ringTimeout())
		next := false
		final := false

		for !next {
			select {
			case r := <-call.ringCh:
				if r.branch != br {
					continue
				}
				switch code := r.resp.StatusCode; {
				case code < 180:
					// 100 Trying, keep waiting.
				case code == 180 || code == 183:
					if !ringing {
						m.respond(buildResponse(req, 180, "Ringing", nil, ""))
						ringing = true
					}
				case code >= 200 && code < 300:
					return br, r.resp
				case code >= 300:
					final = true
					next = true
				}
			case <-call.cancelCh:
				return nil, nil
			case <-deadline:
				next = true
			}
		}
		// Only still-ringing branches need a CANCEL.
		if !final {
			s.cancelRingBranch(req, br)
		}
	}
	return nil, nil
}

// sendRingInvite forks an INVITE to one ring group member.
func (s *Server) sendRingInvite(req *sip.RawMessage, call *Call, reg *Registration) *ringBranch {
	branch := fmt.Sprintf("z9hG4bK%d", time.Now().UnixNano())
	serverVia := fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=%s", s.host, s.port, branch)
	br := &ringBranch{Ext: reg.Ext, Addr: reg.Addr, Via: branch}
	call.addBranch(br)

	uri := fmt.Sprintf("sip:%s@%s", reg.Ext, reg.Addr.String())
	msg := buildRequest("INVITE", uri, req, map[string]string{
		"Via":     serverVia,
		"Contact": fmt.Sprintf("<sip:server@%s:%d>", s.host, s.port),
	}, req.Body)

	s.log.Debug().Str("callID", call.CallID).Str("callee", reg.Ext).Msg("ringing group member")
	s.sendRaw(reg.Addr, msg)
	return br
}

// cancelRingBranch stops one ringing branch.
func (s *Server) cancelRingBranch(req *sip.RawMessage, br *ringBranch) {
	if br == nil || br.Addr == nil {
		return
	}
	serverVia := fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=%s", s.host, s.port, br.Via)
	uri := fmt.Sprintf("sip:%s@%s", br.Ext, br.Addr.String())
	msg := buildRequest("CANCEL", uri, req, map[string]string{
		"Via": serverVia,
	}, "")
	s.sendRaw(br.Addr, msg)
}

// cancelRingBranches stops every ringing branch except keep.
func (s *Server) cancelRingBranches(req *sip.RawMessage, call *Call, keep *ringBranch) {
	for _, br := range call.branchesSnapshot() {
		if br == keep {
			continue
		}
		s.cancelRingBranch(req, br)
	}
}

// bridgeRingWinner makes the winner of a ring group the callee of the call so
// that ACK, BYE and in-dialog requests are routed to it.
func (s *Server) bridgeRingWinner(call *Call, winner *ringBranch) {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	call.CalleeExt = winner.Ext
	call.CalleeAddr = winner.Addr
	call.Answered = true
}

func (s *Server) handleAck(m sipMsg) {
	req := m.msg
	callID := getHeader(req, "Call-ID")
	call := s.getCall(callID)
	if call == nil {
		// best effort: try to route by target? Just log.
		s.log.Debug().Str("callID", callID).Msg("ACK for unknown call")
		return
	}
	if call.Virtual {
		// The server is the only far side; the ACK ends the transaction
		// here and has nowhere to go (GOBELISK-12).
		return
	}
	// Route by dialog leg: the UAC that sent the INVITE (re-INVITE included)
	// sends the ACK, so it must reach the opposite leg (GOBELISK-10).
	leg := call.legOf(m)
	if leg == "" {
		s.log.Debug().Str("callID", callID).Msg("ACK from unknown party")
		return
	}
	other := call.otherAddr(leg)
	if other == nil {
		return
	}
	uri := call.remoteTarget(otherLeg(leg))
	if uri == "" {
		uri = req.Target
	}
	branch := newBranch()
	serverVia := fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=%s", s.host, s.port, branch)
	msg := buildRequest("ACK", uri, req, map[string]string{
		"Via": serverVia,
	}, "")
	s.sendRaw(other, msg)
}

func (s *Server) handleBye(m sipMsg) {
	req := m.msg
	callID := getHeader(req, "Call-ID")
	call := s.getCall(callID)
	// respond 200 to sender
	m.respond(buildResponse(req, 200, "OK", nil, ""))

	if call != nil {
		// The parked party hung up: free the slot instead of forwarding —
		// the server is the far side of this dialog (GOBELISK-12).
		if call.Virtual {
			if slot, ok := s.freeParkByCallID(callID); ok {
				s.log.Info().Str("callID", callID).Int("slot", slot).Msg("parked call hung up")
			}
			s.removeCall(callID)
			return
		}
		// Still ringing a group: stop the forks instead of forwarding.
		if call.CalleeAddr == nil {
			call.cancel()
			s.cancelRingBranches(req, call, nil)
			s.removeCall(callID)
			return
		}
		// Forward to the other party; the dialog tags identify the sender,
		// the source address is the fallback (GOBELISK-10).
		leg := call.legOf(m)
		var other *net.UDPAddr
		var targetLeg string
		switch leg {
		case "caller":
			other, targetLeg = call.CalleeAddr, "callee"
		case "callee":
			other, targetLeg = call.CallerAddr, "caller"
		default:
			if m.addr.String() == call.CallerAddr.String() {
				other, targetLeg = call.CalleeAddr, "callee"
			} else {
				other, targetLeg = call.CallerAddr, "caller"
			}
		}
		if other != nil {
			uri := call.remoteTarget(targetLeg)
			if uri == "" {
				uri = req.Target
			}
			if uri == "" {
				uri = getHeader(req, "Request-URI")
			}
			branch := newBranch()
			serverVia := fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=%s", s.host, s.port, branch)
			msg := buildRequest("BYE", uri, req, map[string]string{
				"Via": serverVia,
			}, "")
			s.sendRaw(other, msg)
		}
		s.removeCall(callID)
	}
}

func (s *Server) handleCancel(m sipMsg) {
	req := m.msg
	callID := getHeader(req, "Call-ID")
	m.respond(buildResponse(req, 200, "OK", nil, ""))

	call := s.getCall(callID)
	if call != nil {
		// Still ringing a group: stop every fork.
		if call.CalleeAddr == nil {
			call.cancel()
			s.cancelRingBranches(req, call, nil)
			s.removeCall(callID)
			return
		}
		// forward CANCEL to callee
		branch := fmt.Sprintf("z9hG4bK%d", time.Now().UnixNano())
		serverVia := fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=%s", s.host, s.port, branch)
		uri := fmt.Sprintf("sip:%s@%s", call.CalleeExt, call.CalleeAddr.String())
		msg := buildRequest("CANCEL", uri, req, map[string]string{
			"Via": serverVia,
		}, "")
		s.sendRaw(call.CalleeAddr, msg)

		// send 487 to caller for original invite
		// reuse req? Build from a synthetic? We'll send Request Terminated
		// Need original INVITE CSeq; use req CSeq but method INVITE? Simpler skip.
		s.removeCall(callID)
	}
}

func (s *Server) routeResponse(m sipMsg) {
	resp := m.msg

	// Responses to outbound trunk requests (REGISTER, OPTIONS) are routed
	// by their branch (GOBELISK-25).
	if s.routeTrunkResponse(resp) {
		return
	}

	// Responses to requests we forwarded (initial INVITE, re-INVITE, UPDATE)
	// go back to the leg that asked for them (GOBELISK-10).
	if tx := s.lookupPendingBranch(topViaBranch(getHeader(resp, "Via"))); tx != nil {
		s.relayPending(tx, resp)
		return
	}

	callID := getHeader(resp, "Call-ID")
	call := s.getCall(callID)
	if call == nil {
		s.log.Debug().Str("callID", callID).Int("code", resp.StatusCode).Msg("unmatched response")
		return
	}

	// Ring group responses are matched to the forked INVITE via its branch.
	if br := call.branchForVia(getHeader(resp, "Via")); br != nil {
		select {
		case call.ringCh <- ringBranchResp{branch: br, resp: resp}:
		default:
			s.log.Debug().Str("callID", callID).Str("ext", br.Ext).Int("code", resp.StatusCode).Msg("ring branch response dropped")
		}
		return
	}

	s.log.Debug().Str("callID", callID).Int("code", resp.StatusCode).Msg("response with no pending transaction")
}

// ---- helpers ----

func (s *Server) getReg(ext string) *Registration {
	s.regMu.Lock()
	defer s.regMu.Unlock()
	r, ok := s.regs[ext]
	if !ok {
		return nil
	}
	if time.Now().After(r.Expires) {
		delete(s.regs, ext)
		return nil
	}
	return r
}

func (s *Server) getCall(id string) *Call {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	return s.calls[id]
}

// extBusy reports whether an extension is currently in an established call.
func (s *Server) extBusy(ext string) bool {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	for _, c := range s.calls {
		if c.Answered && c.CalleeExt == ext {
			return true
		}
	}
	return false
}

// markAnswered marks a call as established.
func (s *Server) markAnswered(call *Call) {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	call.Answered = true
}

// callWaitingEnabled reports whether ext accepts a second call.
func (s *Server) callWaitingEnabled(ext string) bool {
	return s.cfg != nil && s.cfg.CallWaitingEnabled(ext)
}

// ringTimeout is how long a ring group member rings.
func (s *Server) ringTimeout() time.Duration {
	if s.cfg != nil {
		return s.cfg.RingTimeout()
	}
	return 20 * time.Second
}

func (s *Server) removeCall(id string) {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	delete(s.calls, id)
}

func (s *Server) sendRaw(addr *net.UDPAddr, msg string) {
	if s.conn == nil || addr == nil {
		s.log.Error().Msg("cannot send: no conn/addr")
		return
	}
	_, err := s.conn.WriteToUDP([]byte(msg), addr)
	if err != nil {
		s.log.Error().Err(err).Msg("send failed")
	}
}

func (s *Server) sendResponse(addr *net.UDPAddr, msg string) {
	s.sendRaw(addr, msg)
}

// ---- message builders ----

func getHeader(rm *sip.RawMessage, name string) string {
	if rm == nil {
		return ""
	}
	for k, v := range rm.Headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

// topViaBranch extracts the branch parameter from the topmost Via header.
func topViaBranch(via string) string {
	if i := strings.Index(via, ","); i >= 0 {
		via = via[:i]
	}
	for _, part := range strings.Split(via, ";") {
		part = strings.TrimSpace(part)
		if len(part) >= 7 && strings.EqualFold(part[:7], "branch=") {
			return strings.TrimSpace(part[7:])
		}
	}
	return ""
}

// registrationExts returns the extension numbers of the given registrations.
func registrationExts(regs []*Registration) []string {
	out := make([]string, 0, len(regs))
	for _, reg := range regs {
		out = append(out, reg.Ext)
	}
	return out
}

func userFromURI(uri string) string {
	uri = strings.TrimSpace(uri)
	uri = strings.Trim(uri, "<>")
	if !strings.Contains(uri, "@") {
		return ""
	}
	if i := strings.Index(strings.ToLower(uri), "sip:"); i >= 0 {
		uri = uri[i+4:]
	}
	if i := strings.Index(uri, "@"); i >= 0 {
		uri = uri[:i]
	}
	if i := strings.Index(uri, ";"); i >= 0 {
		uri = uri[:i]
	}
	if i := strings.Index(uri, ">"); i >= 0 {
		uri = uri[:i]
	}
	return strings.TrimSpace(uri)
}

func parseContactAddr(contact string) *net.UDPAddr {
	re := regexp.MustCompile(`sip:[^@]+@([^;>]+)`)
	m := re.FindStringSubmatch(contact)
	hostport := ""
	if len(m) > 1 {
		hostport = strings.TrimSpace(m[1])
	}
	if hostport == "" {
		// maybe no sip:
		hostport = strings.TrimSpace(contact)
	}
	if hostport == "" {
		return nil
	}
	if !strings.Contains(hostport, ":") {
		hostport += ":5060"
	}
	addr, err := net.ResolveUDPAddr("udp", hostport)
	if err != nil {
		return nil
	}
	return addr
}

func buildResponse(req *sip.RawMessage, code int, reason string, extra map[string]string, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "SIP/2.0 %d %s\r\n", code, reason)
	if v := getHeader(req, "Via"); v != "" {
		fmt.Fprintf(&b, "Via: %s\r\n", v)
	}
	if v := getHeader(req, "From"); v != "" {
		fmt.Fprintf(&b, "From: %s\r\n", v)
	}
	to := getHeader(req, "To")
	if v, ok := extra["To"]; ok {
		fmt.Fprintf(&b, "To: %s\r\n", v)
	} else if to != "" {
		fmt.Fprintf(&b, "To: %s\r\n", to)
	}
	if v := getHeader(req, "Call-ID"); v != "" {
		fmt.Fprintf(&b, "Call-ID: %s\r\n", v)
	}
	if v := getHeader(req, "CSeq"); v != "" {
		fmt.Fprintf(&b, "CSeq: %s\r\n", v)
	}
	for k, v := range extra {
		if k == "To" {
			continue // already written above
		}
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	if body != "" {
		ct := "application/sdp"
		fmt.Fprintf(&b, "Content-Type: %s\r\n", ct)
	}
	fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	b.WriteString("\r\n")
	if body != "" {
		b.WriteString(body)
	}
	return b.String()
}

// buildResponseFromRaw relays resp to the sender of req, rebuilding the Via
// path from req. contactOverride, when non-empty and resp is a 2xx, replaces
// the Contact so both dialog legs keep addressing this server (GOBELISK-10).
func buildResponseFromRaw(resp *sip.RawMessage, req *sip.RawMessage, contactOverride string) string {
	code := resp.StatusCode
	reason := resp.StatusDesc
	body := resp.Body

	var b strings.Builder
	fmt.Fprintf(&b, "SIP/2.0 %d %s\r\n", code, reason)

	if v := getHeader(req, "Via"); v != "" {
		fmt.Fprintf(&b, "Via: %s\r\n", v)
	}

	from := getHeader(resp, "From")
	if from == "" {
		from = getHeader(req, "From")
	}
	fmt.Fprintf(&b, "From: %s\r\n", from)

	to := getHeader(resp, "To")
	if to == "" {
		to = getHeader(req, "To")
	}
	fmt.Fprintf(&b, "To: %s\r\n", to)

	callid := getHeader(resp, "Call-ID")
	if callid == "" {
		callid = getHeader(req, "Call-ID")
	}
	fmt.Fprintf(&b, "Call-ID: %s\r\n", callid)

	cseq := getHeader(resp, "CSeq")
	if cseq == "" {
		cseq = getHeader(req, "CSeq")
	}
	fmt.Fprintf(&b, "CSeq: %s\r\n", cseq)

	if contactOverride != "" && code >= 200 && code < 300 {
		fmt.Fprintf(&b, "Contact: %s\r\n", contactOverride)
	} else if v := getHeader(resp, "Contact"); v != "" {
		fmt.Fprintf(&b, "Contact: %s\r\n", v)
	}
	if v := getHeader(resp, "Record-Route"); v != "" {
		fmt.Fprintf(&b, "Record-Route: %s\r\n", v)
	}

	if body != "" {
		ct := getHeader(resp, "Content-Type")
		if ct == "" {
			ct = "application/sdp"
		}
		fmt.Fprintf(&b, "Content-Type: %s\r\n", ct)
	}
	fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	b.WriteString("\r\n")
	if body != "" {
		b.WriteString(body)
	}
	return b.String()
}

// localIP returns a non-loopback IPv4 address for this machine, if available.
func localIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	if localAddr.IP.IsLoopback() {
		return "127.0.0.1"
	}
	return localAddr.IP.String()
}

func buildRequest(method, uri string, orig *sip.RawMessage, overrides map[string]string, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s SIP/2.0\r\n", method, uri)

	if v, ok := overrides["Via"]; ok {
		fmt.Fprintf(&b, "Via: %s\r\n", v)
	} else if v := getHeader(orig, "Via"); v != "" {
		fmt.Fprintf(&b, "Via: %s\r\n", v)
	}

	if v, ok := overrides["Max-Forwards"]; ok {
		fmt.Fprintf(&b, "Max-Forwards: %s\r\n", v)
	} else {
		fmt.Fprintf(&b, "Max-Forwards: 70\r\n")
	}

	if v := getHeader(orig, "From"); v != "" {
		fmt.Fprintf(&b, "From: %s\r\n", v)
	}
	if v := getHeader(orig, "To"); v != "" {
		fmt.Fprintf(&b, "To: %s\r\n", v)
	}
	if v := getHeader(orig, "Call-ID"); v != "" {
		fmt.Fprintf(&b, "Call-ID: %s\r\n", v)
	}

	cseq := getHeader(orig, "CSeq")
	parts := strings.Fields(cseq)
	if len(parts) >= 1 {
		cseq = parts[0] + " " + method
	} else {
		cseq = "1 " + method
	}
	fmt.Fprintf(&b, "CSeq: %s\r\n", cseq)

	if v, ok := overrides["Contact"]; ok {
		fmt.Fprintf(&b, "Contact: %s\r\n", v)
	}

	// Any other supplied header (for example Alert-Info) is passed through.
	for k, v := range overrides {
		switch k {
		case "Via", "Max-Forwards", "Contact":
			continue
		}
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}

	// End-to-end headers that must survive forwarding: transfers (RFC 5589 /
	// 3891), event packages (NOTIFY) and extension negotiation (GOBELISK-11).
	for _, name := range []string{"Refer-To", "Referred-By", "Replaces", "Event",
		"Subscription-State", "Allow-Events", "Require"} {
		if _, ok := overrides[name]; ok {
			continue
		}
		if v := getHeader(orig, name); v != "" {
			fmt.Fprintf(&b, "%s: %s\r\n", name, v)
		}
	}

	if body != "" {
		if ct := getHeader(orig, "Content-Type"); ct != "" {
			// Carry the original content type (application/sipfrag, text/plain...)
			fmt.Fprintf(&b, "Content-Type: %s\r\n", ct)
		} else {
			fmt.Fprintf(&b, "Content-Type: application/sdp\r\n")
		}
	}
	fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	b.WriteString("\r\n")
	if body != "" {
		b.WriteString(body)
	}
	return b.String()
}

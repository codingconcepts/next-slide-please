// Package present runs the presenter half of nsp: the machine with the
// slides on it. It holds a signalling socket open for the whole talk,
// answers each speaker that arrives with its own peer connection, and
// presses the keys they send - but only for the speakers the presenter has
// left enabled.
package present

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/codingconcepts/next-slide-please/internal/machine"
	"github.com/codingconcepts/next-slide-please/internal/rtc"
	"github.com/codingconcepts/next-slide-please/internal/signal"
	"github.com/pion/webrtc/v4"
)

// keepAliveInterval is how often the presenter pings the signalling server
// while waiting to be joined.
const keepAliveInterval = 30 * time.Second

// Event is something the session wants the UI to show. The session owns the
// connections and the enabled flags; the UI owns the display and learns about
// changes only through these.
type Event any

type (
	// Joined is a speaker that has offered to connect but whose data channel
	// is not open yet. Local marks a speaker running on the presenter's own
	// machine, which arrives muted rather than logging.
	Joined struct {
		ID    string
		Name  string
		Mode  rtc.Mode
		Local bool
	}

	// Ready is a speaker whose data channel is open and who can now drive.
	Ready struct{ ID string }

	// Left is a speaker that has gone, whether it dropped out of signalling
	// or its peer connection went down.
	Left struct{ ID string }

	// Key is a command that arrived from a speaker, and what became of it:
	// pressed, logged and no more, or dropped because that speaker is muted.
	Key struct {
		ID   string
		Key  string
		Mode rtc.Mode
	}

	// Failed means the session itself is over, not a single speaker.
	Failed struct{ Err error }
)

// Presser applies a key on the presenter's machine. A nil Presser is
// --dry-run: keys still arrive and are still reported, but nothing is typed
// anywhere. A session that types nothing cannot start a feedback loop, which
// is why it is also the one that lets a speaker on this machine drive.
type Presser func(key string) error

// Option adjusts a session as it is built.
type Option func(*Session)

// WithSameMachine replaces the test for whether a speaker is running
// alongside the presenter. The end to end tests have no choice but to run
// both halves in one process, so they turn it off to reach the paths a real
// talk takes; nothing else should want this.
func WithSameMachine(fn func(fingerprint string) bool) Option {
	return func(s *Session) { s.sameMachine = fn }
}

// Session is the presenter's view of a room.
type Session struct {
	code        string
	server      string
	press       Presser
	sameMachine func(fingerprint string) bool
	config      webrtc.Configuration
	ws          *signal.Conn
	events      chan Event

	mu    sync.Mutex
	peers map[string]*peer

	// Closing a peer connection makes pion fire its state change callbacks,
	// on its own goroutines, after Run has already given up. Those callbacks
	// still want to emit, so the channel's lifetime needs a lock of its own
	// rather than being tied to Run returning.
	eventsMu sync.Mutex
	closed   bool
}

type peer struct {
	pc   *webrtc.PeerConnection
	dc   *webrtc.DataChannel
	mode rtc.Mode

	// Whether this speaker is on the presenter's own machine, which is the
	// one thing that rules a mode out rather than merely making it unwise.
	local bool
}

// New fetches ICE servers and claims code on the signalling server, so that
// by the time it returns the room exists and speakers can join it.
func New(ctx context.Context, server, code string, press Presser, opts ...Option) (*Session, error) {
	config, err := signal.ICEConfig(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("fetching ice servers: %w", err)
	}

	ws, err := signal.DialPresenter(ctx, server, code)
	if err != nil {
		return nil, fmt.Errorf("waiting on code %q: %w", code, err)
	}

	session := &Session{
		code:        code,
		server:      server,
		press:       press,
		sameMachine: machine.SameAs,
		config:      config,
		ws:          ws,
		// Buffered so a burst of keypresses cannot block the data channel
		// callback that produced them.
		events: make(chan Event, 64),
		peers:  make(map[string]*peer),
	}

	for _, opt := range opts {
		opt(session)
	}

	return session, nil
}

func (s *Session) Code() string { return s.code }

// Events is closed when the session ends.
func (s *Session) Events() <-chan Event { return s.events }

// Run reads the signalling socket until ctx is cancelled or the server hangs
// up. Each speaker is answered on its own goroutine, because gathering ICE
// candidates for one takes a second or two and must not hold up the next
// arrival.
func (s *Session) Run(ctx context.Context) {
	// Ordered so the connections are torn down first and the events that
	// teardown produces still have somewhere to go.
	defer s.closeEvents()
	defer s.closeAll()

	go s.keepAlive(ctx)

	for {
		envelope, err := s.ws.Read(ctx)
		if err != nil {
			if ctx.Err() == nil {
				s.emit(Failed{Err: fmt.Errorf("signalling connection lost: %w", err)})
			}

			return
		}

		switch envelope.Kind {
		case signal.KindOffer:
			local := s.isLocal(envelope.Host)

			s.emit(Joined{
				ID:    envelope.Peer,
				Name:  envelope.Name,
				Mode:  arrivingMode(local),
				Local: local,
			})
			go s.answer(ctx, envelope)

		case signal.KindLeave:
			s.dropIfIdle(envelope.Peer)
		}
	}
}

// keepAlive stops the presenter's socket being reaped by something in the
// middle while it sits waiting to be joined. Unlike a speaker, the presenter
// holds its socket for the whole talk, and the gaps between people arriving
// are long enough for an idle proxy to lose interest. Cloudflare answers a
// ping without waking the room, so this does not cost hibernation.
func (s *Session) keepAlive(ctx context.Context) {
	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, keepAliveInterval)
			err := s.ws.Ping(pingCtx)

			cancel()

			// A dead socket surfaces through Run's read loop, which owns the
			// session's lifetime. Nothing useful to do here.
			if err != nil {
				return
			}
		}
	}
}

// answer brings up one peer connection and sends its SDP back to the single
// speaker that asked for it.
func (s *Session) answer(ctx context.Context, envelope signal.Envelope) {
	id := envelope.Peer

	if envelope.SDP == nil {
		s.fail(id, fmt.Errorf("speaker %q offered no sdp", envelope.Name))
		return
	}

	pc, err := rtc.NewPeerConnection(s.config)
	if err != nil {
		s.emit(Failed{Err: fmt.Errorf("creating peer connection: %w", err)})
		return
	}

	local := s.isLocal(envelope.Host)

	s.mu.Lock()
	s.peers[id] = &peer{pc: pc, mode: arrivingMode(local), local: local}
	s.mu.Unlock()

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != rtc.ChannelLabel {
			return
		}

		dc.OnOpen(func() {
			s.mu.Lock()

			p, ok := s.peers[id]
			if ok {
				p.dc = dc
			}
			s.mu.Unlock()

			if !ok {
				return
			}

			s.emit(Ready{ID: id})
			s.notify(id)
		})

		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			s.onKey(id, string(msg.Data))
		})
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateDisconnected,
			webrtc.PeerConnectionStateClosed:
			s.drop(id)
		}
	})

	if err = pc.SetRemoteDescription(*envelope.SDP); err != nil {
		s.fail(id, fmt.Errorf("setting remote description: %w", err))
		return
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		s.fail(id, fmt.Errorf("creating answer: %w", err))
		return
	}

	if err = rtc.LocalDescription(ctx, pc, answer); err != nil {
		s.fail(id, err)
		return
	}

	connectCtx, cancel := context.WithTimeout(ctx, rtc.ConnectTimeout)
	defer cancel()

	if err = s.ws.Write(connectCtx, signal.Envelope{
		Kind: signal.KindAnswer,
		Peer: id,
		SDP:  pc.LocalDescription(),
	}); err != nil {
		s.fail(id, fmt.Errorf("sending answer: %w", err))
	}
}

// onKey is the only path from the network to the keyboard. A message is
// pressed only if it is one of the two keys nsp carries and the speaker that
// sent it has been promoted all the way to enabled.
func (s *Session) onKey(id, msg string) {
	if !rtc.IsKey(msg) {
		return
	}

	s.mu.Lock()
	p, ok := s.peers[id]

	var mode rtc.Mode
	if ok {
		mode = s.effective(p.mode)
	}
	s.mu.Unlock()

	if !ok {
		return
	}

	if mode != rtc.ModeEnabled {
		s.emit(Key{ID: id, Key: msg, Mode: mode})
		return
	}

	if err := s.press(msg); err != nil {
		s.emit(Failed{Err: fmt.Errorf("pressing %q: %w", msg, err)})
		return
	}

	s.emit(Key{ID: id, Key: msg, Mode: mode})
}

// effective is what a mode actually amounts to on this session. Under
// --dry-run there is no presser, so a speaker the presenter has enabled is
// really only being logged, and saying so beats a list that claims keys are
// being pressed when nothing is.
func (s *Session) effective(mode rtc.Mode) rtc.Mode {
	if mode == rtc.ModeEnabled && s.press == nil {
		return rtc.ModeLogging
	}

	return mode
}

// arrivingMode is the mode a speaker joins in.
//
// Logging rather than enabled: holding the code gets you into the room, and
// the presenter decides who drives. Muted rather than logging for a speaker
// on the presenter's own machine, which is one keystroke further from
// driving because a key pressed for it would land in whichever window has
// focus - quite possibly that speaker's own terminal, which would read it as
// input and send it straight back.
func arrivingMode(local bool) rtc.Mode {
	if local {
		return rtc.ModeMuted
	}

	return rtc.ModeLogging
}

// isLocal reports whether a speaker is running on this machine. A presenter
// that presses nothing has nothing to feed back, so under --dry-run a local
// speaker is as welcome as any other.
func (s *Session) isLocal(fingerprint string) bool {
	return s.press != nil && s.sameMachine(fingerprint)
}

// SetMode decides what happens to a single speaker's keys from now on and
// tells that speaker about it, so their own display can say whether they are
// driving.
//
// A speaker on this machine is held to logging at most, here rather than
// only in the UI: the session owns the keyboard, so the rule that protects
// it belongs where nothing can route around it.
func (s *Session) SetMode(id string, mode rtc.Mode) {
	s.mu.Lock()
	if p, ok := s.peers[id]; ok {
		p.mode = rtc.Allow(mode, p.local)
	}
	s.mu.Unlock()

	s.notify(id)
}

// notify tells a speaker what is being done with its keys. Best effort: a
// speaker whose channel has gone simply does not hear about it.
func (s *Session) notify(id string) {
	s.mu.Lock()
	p, ok := s.peers[id]

	var (
		dc   *webrtc.DataChannel
		mode rtc.Mode
	)
	if ok {
		dc, mode = p.dc, s.effective(p.mode)
	}
	s.mu.Unlock()

	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}

	_ = dc.SendText(string(mode))
}

func (s *Session) fail(id string, err error) {
	s.emit(Failed{Err: err})
	s.drop(id)
}

// dropIfIdle handles a speaker whose signalling socket has closed. That is
// not on its own a reason to give up on them: a speaker closes its socket the
// moment its data channel opens, precisely so that nothing about the talk
// keeps touching Cloudflare. So this only clears up speakers whose peer
// connection never got going. Once ICE is under way the connection state is
// the authority on whether they are still there.
func (s *Session) dropIfIdle(id string) {
	s.mu.Lock()
	p, ok := s.peers[id]
	s.mu.Unlock()

	if !ok {
		return
	}

	switch p.pc.ConnectionState() {
	case webrtc.PeerConnectionStateNew, webrtc.PeerConnectionStateFailed:
		s.drop(id)
	}
}

// drop is idempotent, because a speaker going away is reported twice: once by
// the signalling server and once by the peer connection changing state.
func (s *Session) drop(id string) {
	s.mu.Lock()
	p, ok := s.peers[id]
	delete(s.peers, id)
	s.mu.Unlock()

	if !ok {
		return
	}

	_ = p.pc.Close()

	s.emit(Left{ID: id})
}

func (s *Session) closeAll() {
	s.mu.Lock()
	peers := s.peers
	s.peers = make(map[string]*peer)
	s.mu.Unlock()

	for _, p := range peers {
		_ = p.pc.Close()
	}

	s.ws.CloseNow()
}

// emit never blocks. Events are display only, so dropping one under a flood
// is better than stalling the callback that produced it. Emitting after the
// session has finished is a no-op rather than a panic, because teardown
// itself produces events.
func (s *Session) emit(event Event) {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()

	if s.closed {
		return
	}

	select {
	case s.events <- event:
	default:
	}
}

// closeEvents ends the stream. Safe to call more than once.
func (s *Session) closeEvents() {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()

	if s.closed {
		return
	}

	s.closed = true
	close(s.events)
}

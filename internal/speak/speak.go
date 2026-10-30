// Package speak runs the speaker half of nsp: the person asking for the next
// slide. It connects to one presenter and sends the two keys nsp carries.
//
// Nothing here touches the speaker's keyboard globally. Keys come from the
// TUI's own input, which is why running nsp as a speaker needs no special
// permission and does not interfere with anything else on the machine.
package speak

import (
	"context"
	"fmt"

	"github.com/codingconcepts/next-slide-please/internal/machine"
	"github.com/codingconcepts/next-slide-please/internal/rtc"
	"github.com/codingconcepts/next-slide-please/internal/signal"
	"github.com/pion/webrtc/v4"
)

// Event is something the session wants the UI to show.
type Event any

type (
	// Connected means the data channel to the presenter is open.
	Connected struct{}

	// Mode carries the presenter's verdict on what is being done with this
	// speaker's keys: pressed, logged and no more, or dropped.
	Mode struct{ Mode rtc.Mode }

	// Closed means the session is over.
	Closed struct{ Err error }
)

// Session is one speaker's connection to one presenter.
type Session struct {
	pc     *webrtc.PeerConnection
	dc     *webrtc.DataChannel
	events chan Event
}

// Connect performs the whole handshake and returns once the data channel is
// open, so a session that comes back is one that can drive slides.
func Connect(ctx context.Context, server, code, name string) (*Session, error) {
	config, err := signal.ICEConfig(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("fetching ice servers: %w", err)
	}

	pc, err := rtc.NewPeerConnection(config)
	if err != nil {
		return nil, fmt.Errorf("creating peer connection: %w", err)
	}

	s := &Session{
		pc:     pc,
		events: make(chan Event, 8),
	}

	dc, err := pc.CreateDataChannel(rtc.ChannelLabel, nil)
	if err != nil {
		pc.Close()
		return nil, fmt.Errorf("creating data channel: %w", err)
	}

	s.dc = dc

	// Registered before the channel has any chance to open, so the
	// notification cannot be missed.
	open := make(chan struct{})

	dc.OnOpen(func() { close(open) })
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if mode, ok := rtc.ParseMode(string(msg.Data)); ok {
			s.emit(Mode{Mode: mode})
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		pc.Close()
		return nil, fmt.Errorf("creating offer: %w", err)
	}

	if err = rtc.LocalDescription(ctx, pc, offer); err != nil {
		pc.Close()
		return nil, err
	}

	ws, err := signal.DialSpeaker(ctx, server, code, name, machine.Fingerprint())
	if err != nil {
		pc.Close()
		return nil, fmt.Errorf("joining with code %q: %w", code, err)
	}
	defer ws.CloseNow()

	connectCtx, cancel := context.WithTimeout(ctx, rtc.ConnectTimeout)
	defer cancel()

	if err = ws.Write(connectCtx, signal.Envelope{
		Kind: signal.KindOffer,
		SDP:  pc.LocalDescription(),
	}); err != nil {
		pc.Close()
		return nil, fmt.Errorf("sending offer: %w", err)
	}

	answer, err := ws.Read(connectCtx)
	if err != nil {
		pc.Close()
		return nil, fmt.Errorf("waiting for the presenter to answer: %w", err)
	}

	if answer.Kind != signal.KindAnswer || answer.SDP == nil {
		pc.Close()
		return nil, fmt.Errorf("the presenter sent something other than an answer")
	}

	if err = pc.SetRemoteDescription(*answer.SDP); err != nil {
		pc.Close()
		return nil, fmt.Errorf("setting remote description: %w", err)
	}

	// The socket is held until the data channel is up, not dropped the moment
	// the answer arrives: the presenter treats a speaker's signalling socket
	// closing early as that speaker giving up, and would tear down the very
	// connection being negotiated.
	select {
	case <-open:
	case <-connectCtx.Done():
		pc.Close()
		return nil, fmt.Errorf("timed out establishing a direct connection to the presenter")
	}

	// Signalling is finished, so drop the connection to Cloudflare entirely.
	// From here on nothing about this talk touches their network.
	ws.Close()

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateDisconnected,
			webrtc.PeerConnectionStateClosed:
			s.emit(Closed{Err: fmt.Errorf("the presenter went away")})
		}
	})

	s.emit(Connected{})

	return s, nil
}

// Events is the stream of state changes for the UI to draw.
func (s *Session) Events() <-chan Event { return s.events }

// Send asks the presenter to press a key. It is a no-op for anything other
// than the two keys nsp carries, and the presenter checks again at its end.
func (s *Session) Send(key string) error {
	if !rtc.IsKey(key) {
		return nil
	}

	if s.dc.ReadyState() != webrtc.DataChannelStateOpen {
		return fmt.Errorf("not connected to the presenter")
	}

	return s.dc.SendText(key)
}

func (s *Session) Close() {
	s.pc.Close()
}

func (s *Session) emit(event Event) {
	select {
	case s.events <- event:
	default:
	}
}

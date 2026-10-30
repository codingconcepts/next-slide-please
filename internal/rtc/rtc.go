// Package rtc holds the peer connection plumbing shared by both halves of
// nsp.
package rtc

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

// NewPeerConnection builds a peer connection with the candidate filtering
// both halves of nsp need. Always use this rather than
// webrtc.NewPeerConnection, or a laptop with a few virtual interfaces will
// negotiate a connection that looks established and then carries nothing -
// or, worse, will not negotiate one at all.
func NewPeerConnection(config webrtc.Configuration) (*webrtc.PeerConnection, error) {
	var settings webrtc.SettingEngine
	settings.SetIPFilter(usable)
	settings.SetInterfaceFilter(usableInterface)

	return webrtc.NewAPI(webrtc.WithSettingEngine(settings)).NewPeerConnection(config)
}

// usable drops addresses that cannot carry traffic to another machine.
//
// A link-local address is only routable on the link it belongs to, but ICE
// will happily pair a local fe80:: candidate with a peer's global address,
// pass its connectivity checks and report itself connected. Nothing can
// actually flow over that pair, so DTLS sits in "connecting" until consent
// freshness gives up thirty seconds later - a failure that looks like a
// hang rather than a rejection. Developer machines tend to collect these
// addresses, along with VM bridges and VPN interfaces, so the more
// candidates in play the likelier ICE is to choose a dud.
func usable(ip net.IP) bool {
	return !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsInterfaceLocalMulticast()
}

// virtualInterfaces are the interface names a machine collects that are not
// a way out to the internet: VM bridges, VPN tunnels, Apple's peer to peer
// and baseband links, and the IPv6 tunnelling pseudo-interfaces.
var virtualInterfaces = []string{
	"anpi",   // apple baseband
	"ap",     // apple wifi access point
	"awdl",   // apple wireless direct link
	"bridge", // vm and container bridges
	"gif",    // ipv6 over ipv4 tunnel
	"llw",    // apple low latency wlan
	"stf",    // 6to4 tunnel
	"utun",   // vpn tunnels, tailscale included
	"vmnet",  // vmware and friends
}

// usableInterface keeps ICE off interfaces that cannot reach another machine.
//
// Filtering these by address is not enough, because the damage is done before
// a candidate is ever paired. Gathering on an interface with nowhere to send
// - an idle VM bridge is the usual culprit - leaves pion blocked in a UDP
// write that never completes. That write happens on the ICE agent's single
// task loop, so one wedged socket stops the agent doing anything else ever
// again: connectivity checks stop, and so does reading back the local
// description, which is what the presenter needs before it can answer a
// speaker at all. The speaker then waits out its timeout and reports that the
// presenter never answered, while the presenter sits there apparently fine.
func usableInterface(name string) bool {
	for _, prefix := range virtualInterfaces {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}

	return true
}

// ConnectTimeout bounds everything after a peer has been found: SDP exchange,
// ICE gathering and the data channel opening.
const ConnectTimeout = 30 * time.Second

// ChannelLabel is the data channel slide commands travel over.
const ChannelLabel = "slides"

// Commands a speaker may send. The peer is whoever holds the code, so the
// data channel is not a licence to synthesise arbitrary input: the presenter
// checks every message against this list before pressing anything.
const (
	KeyLeft  = "left"
	KeyRight = "right"
)

// Mode is what the presenter currently does with one speaker's keys. It
// doubles as the control message sent back down that speaker's data channel,
// so the speaker's own display can say what is happening to its keypresses
// rather than guess.
type Mode string

const (
	// ModeMuted drops the speaker's keys on arrival.
	ModeMuted Mode = "nsp:muted"

	// ModeLogging shows the speaker's keys in the presenter's list without
	// pressing them. New speakers start here: holding the code gets you into
	// the room, and the presenter decides who drives.
	ModeLogging Mode = "nsp:logging"

	// ModeEnabled presses the speaker's keys for real.
	ModeEnabled Mode = "nsp:enabled"
)

// ParseMode reads a control message, falling back to muted for anything it
// does not recognise: a speaker that cannot tell what is happening to its
// keys should assume the quieter answer.
func ParseMode(msg string) (Mode, bool) {
	switch mode := Mode(msg); mode {
	case ModeMuted, ModeLogging, ModeEnabled:
		return mode, true
	}

	return ModeMuted, false
}

// Next cycles to the mode the space bar should land on, which is the order
// the presenter's help line promises: muted, logging, enabled, round again.
//
// A speaker on the presenter's own machine has no enabled to reach, so it
// cycles between muted and logging alone. Pressing its keys for real is the
// one thing nsp cannot do safely, and a mode you can land on by holding the
// space bar down is not a deliberate enough way to ask for it.
func (m Mode) Next(local bool) Mode {
	switch {
	case m == ModeMuted:
		return ModeLogging
	case m == ModeLogging && !local:
		return ModeEnabled
	default:
		return ModeMuted
	}
}

// Allow is mode as it may actually be applied to a speaker. Asking for
// enabled on the presenter's own machine gets logging: the keys are shown,
// which is all that speaker was ever going to be allowed, rather than the
// request being silently ignored.
func Allow(mode Mode, local bool) Mode {
	if local && mode == ModeEnabled {
		return ModeLogging
	}

	return mode
}

// IsKey reports whether msg is one of the two keys nsp exists to carry.
func IsKey(msg string) bool {
	return msg == KeyLeft || msg == KeyRight
}

// GatherTimeout caps how long we wait for ICE gathering before sending the
// SDP with whatever candidates we have.
//
// Gathering is not finished until every configured server has either
// answered or timed out, and the TURN config includes URLs on ports that a
// lot of networks drop. Waiting for those to give up takes the best part of
// ten seconds, during which nothing new arrives: in practice the host,
// server reflexive and relay candidates that actually get used all turn up
// within a tenth of a second. So the wait is bounded, generously, and the
// stragglers are abandoned.
const GatherTimeout = 3 * time.Second

// LocalDescription applies desc and waits for ICE gathering, so the SDP
// handed to the peer carries every candidate we know about. Simpler than
// trickling candidates over the signalling channel, at the cost of up to
// GatherTimeout of setup.
func LocalDescription(ctx context.Context, pc *webrtc.PeerConnection, desc webrtc.SessionDescription) error {
	// Must be created before gathering starts.
	gathered := webrtc.GatheringCompletePromise(pc)

	if err := pc.SetLocalDescription(desc); err != nil {
		return fmt.Errorf("setting local description: %w", err)
	}

	timeout := time.NewTimer(GatherTimeout)
	defer timeout.Stop()

	select {
	case <-gathered:
	case <-timeout.C:
	case <-ctx.Done():
		return fmt.Errorf("gathering candidates: %w", ctx.Err())
	}

	return nil
}

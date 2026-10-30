// Package signal talks to the Cloudflare Worker that introduces peers to each
// other. It only ever carries SDP: once a peer connection is established,
// slide commands travel directly between the two machines.
package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/pion/webrtc/v4"
)

// DefaultServer is the hosted signalling Worker. Override it with --server to
// point at your own deployment.
//
// This is compiled into every binary, so changing it only reaches people who
// upgrade. The Worker keeps its workers.dev hostname enabled alongside this
// one as a fallback; both names route to the same script and therefore the
// same Durable Object namespace, so a presenter and a speaker given
// different hostnames still meet in the same room.
const DefaultServer = "https://signal.nextslideplease.run"

// Kind identifies what an envelope carries.
type Kind string

const (
	// KindOffer reaches the presenter when a speaker arrives. Peer, Name and
	// Host identify the speaker; the Worker fills all three in from the
	// speaker's own socket, so a speaker cannot claim to be another one.
	KindOffer Kind = "offer"

	// KindAnswer is the presenter's reply to a single speaker.
	KindAnswer Kind = "answer"

	// KindLeave reaches the presenter when a speaker's signalling socket
	// closes before its peer connection came up.
	KindLeave Kind = "leave"
)

// Envelope is a single message on the signalling socket. A room holds one
// presenter and many speakers, so each message names the speaker it concerns.
type Envelope struct {
	Kind Kind                       `json:"kind"`
	Peer string                     `json:"peer,omitempty"`
	Name string                     `json:"name,omitempty"`
	Host string                     `json:"host,omitempty"`
	SDP  *webrtc.SessionDescription `json:"sdp,omitempty"`
}

// Conn is a signalling socket. The presenter holds one open for the whole
// talk, since speakers can arrive at any point during it. A speaker closes
// its own as soon as it has an answer.
type Conn struct {
	ws *websocket.Conn

	// The presenter answers speakers concurrently, because gathering ICE
	// candidates for one must not hold up the next arrival. A websocket
	// tolerates exactly one writer at a time.
	writeMu sync.Mutex
}

// DialPresenter opens the room identified by code and waits there for
// speakers. It fails if a presenter is already holding that code.
func DialPresenter(ctx context.Context, server, code string) (*Conn, error) {
	return dial(ctx, server, url.Values{
		"code": {code},
		"role": {"presenter"},
	})
}

// DialSpeaker joins the room identified by code. Name is what the presenter
// sees in its list of speakers, and host is the speaker's machine
// fingerprint, which lets the presenter recognise a speaker running
// alongside it.
func DialSpeaker(ctx context.Context, server, code, name, host string) (*Conn, error) {
	return dial(ctx, server, url.Values{
		"code": {code},
		"role": {"speaker"},
		"name": {name},
		"host": {host},
	})
}

// dial reports the server's rejection reason rather than a bare handshake
// error, because the interesting failures - no presenter on that code, code
// already taken - are all refusals before the websocket upgrade.
func dial(ctx context.Context, server string, params url.Values) (*Conn, error) {
	endpoint := fmt.Sprintf("%s/ws?%s",
		strings.Replace(Server(server), "http", "ws", 1), params.Encode())

	ws, res, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		if res != nil {
			return nil, fmt.Errorf("%s", strings.TrimSpace(statusText(res)))
		}

		return nil, fmt.Errorf("reaching the signalling server: %w", err)
	}

	// An SDP blob with a lot of candidates comfortably exceeds the default.
	ws.SetReadLimit(1 << 20)

	return &Conn{ws: ws}, nil
}

func (c *Conn) Read(ctx context.Context) (Envelope, error) {
	_, data, err := c.ws.Read(ctx)
	if err != nil {
		return Envelope{}, err
	}

	var envelope Envelope
	if err = json.Unmarshal(data, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("decoding signalling message: %w", err)
	}

	return envelope, nil
}

func (c *Conn) Write(ctx context.Context, envelope Envelope) error {
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encoding signalling message: %w", err)
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	return c.ws.Write(ctx, websocket.MessageText, data)
}

// Ping keeps an idle socket alive. It must be called concurrently with a
// reader, since the pong is delivered through that reader.
func (c *Conn) Ping(ctx context.Context) error {
	return c.ws.Ping(ctx)
}

func (c *Conn) Close() {
	c.ws.Close(websocket.StatusNormalClosure, "")
}

func (c *Conn) CloseNow() {
	c.ws.CloseNow()
}

// ICEConfig asks the signalling server for STUN and, if it has a key
// configured, short-lived TURN credentials.
func ICEConfig(ctx context.Context, server string) (webrtc.Configuration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Server(server)+"/ice", nil)
	if err != nil {
		return webrtc.Configuration{}, fmt.Errorf("building request: %w", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return webrtc.Configuration{}, fmt.Errorf("requesting ice servers: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return webrtc.Configuration{}, fmt.Errorf("requesting ice servers: %s", res.Status)
	}

	var config webrtc.Configuration
	if err = json.NewDecoder(res.Body).Decode(&config); err != nil {
		return webrtc.Configuration{}, fmt.Errorf("decoding ice servers: %w", err)
	}

	if drop := os.Getenv("NSP_TURN_DROP"); drop != "" {
		for i, srv := range config.ICEServers {
			if srv.Credential == nil {
				continue
			}

			var urls []string

			for _, u := range srv.URLs {
				if !strings.Contains(u, drop) {
					urls = append(urls, u)
				}
			}

			config.ICEServers[i].URLs = urls
		}

		var nonEmpty []webrtc.ICEServer

		for _, srv := range config.ICEServers {
			if len(srv.URLs) > 0 {
				nonEmpty = append(nonEmpty, srv)
			}
		}

		config.ICEServers = nonEmpty
	}

	return config, nil
}

// Server normalises a --server value so the rest of the package can append
// paths to it without worrying about a trailing slash.
func Server(server string) string {
	return strings.TrimSuffix(server, "/")
}

// statusText prefers the Worker's explanation of a refusal to the bare status
// line. On a failed handshake coder/websocket leaves the first kilobyte of the
// body readable for exactly this.
func statusText(res *http.Response) string {
	if res.Body == nil {
		return res.Status
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil || len(body) == 0 {
		return res.Status
	}

	return string(body)
}

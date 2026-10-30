import { DurableObject } from "cloudflare:workers";

export interface Env {
  ROOMS: DurableObjectNamespace<Room>;
  TURN_KEY_ID?: string;
  TURN_KEY_SECRET?: string;
}

type Role = "presenter" | "speaker";

// Abandoned rooms are reaped rather than left holding sockets open forever.
const ROOM_TTL_SECONDS = 2 * 60 * 60;

// Long enough for any talk, well inside the 48h maximum the API allows.
const TURN_TTL_SECONDS = 3600;

// A room with more speakers than this is not a talk, it is an attack.
const MAX_SPEAKERS = 16;

const MAX_NAME_LENGTH = 32;

const STUN_ONLY = {
  iceServers: [{ urls: ["stun:stun.cloudflare.com:3478"] }],
};

// Envelope is everything that crosses the signalling socket. A room holds one
// presenter and many speakers, so each message names the speaker it concerns
// and the presenter answers each one individually.
//
//   speaker  -> { kind: "offer",  sdp }
//   -> presenter { kind: "offer",  peer, name, host, sdp }
//   presenter -> { kind: "answer", peer, sdp }
//   -> speaker   { kind: "answer", sdp }
//   -> presenter { kind: "leave",  peer }
interface Envelope {
  kind: "offer" | "answer" | "leave";
  peer?: string;
  name?: string;
  host?: string;
  sdp?: unknown;
}

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    const url = new URL(req.url);

    switch (url.pathname) {
      case "/ice":
        return iceServers(env);

      case "/ws": {
        const code = url.searchParams.get("code");
        if (!code) {
          return new Response("missing code", { status: 400 });
        }

        const role = url.searchParams.get("role");
        if (role !== "presenter" && role !== "speaker") {
          return new Response("role must be presenter or speaker", { status: 400 });
        }

        return env.ROOMS.get(env.ROOMS.idFromName(code)).fetch(req);
      }

      default:
        return new Response("not found", { status: 404 });
    }
  },
} satisfies ExportedHandler<Env>;

// iceServers mints short-lived TURN credentials. The account secret never
// leaves the Worker; clients only ever see a credential that expires within
// the hour. Falls back to STUN alone if no TURN key is configured, which is
// enough for most connections.
//
// generate-ice-servers already returns the exact shape a peer connection
// wants, so the response is passed straight through. It includes the port 53
// URLs, which browsers block but a native client can happily use - they get
// through a lot of hostile conference wifi.
async function iceServers(env: Env): Promise<Response> {
  if (!env.TURN_KEY_ID || !env.TURN_KEY_SECRET) {
    return Response.json(STUN_ONLY);
  }

  const res = await fetch(
    `https://rtc.live.cloudflare.com/v1/turn/keys/${env.TURN_KEY_ID}/credentials/generate-ice-servers`,
    {
      method: "POST",
      headers: {
        Authorization: `Bearer ${env.TURN_KEY_SECRET}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ ttl: TURN_TTL_SECONDS }),
    },
  );

  if (!res.ok) {
    console.error(`turn credential generation failed with status ${res.status}`);
    return Response.json(STUN_ONLY);
  }

  return new Response(res.body, {
    headers: { "content-type": "application/json" },
  });
}

// Room is the rendezvous point for a single code. Every peer derives the same
// object id from the code, so they meet here regardless of which Cloudflare
// location they hit. It relays SDP and nothing else - once a peer connection
// is up, slide commands travel directly and never come back through here.
//
// The presenter holds its socket open for the whole talk, because speakers can
// arrive at any point during it. That costs nothing: the object hibernates
// between arrivals, which is almost all of the time.
export class Room extends DurableObject<Env> {
  async fetch(req: Request): Promise<Response> {
    if (req.headers.get("Upgrade") !== "websocket") {
      return new Response("expected websocket upgrade", { status: 426 });
    }

    const url = new URL(req.url);

    // Already validated by the Worker before it routed to this object.
    const role = url.searchParams.get("role") as Role;

    if (role === "presenter" && this.ctx.getWebSockets("presenter").length > 0) {
      return new Response("a presenter is already connected with this code", {
        status: 409,
      });
    }

    if (role === "speaker") {
      if (this.ctx.getWebSockets("presenter").length === 0) {
        return new Response("no presenter is waiting on this code", { status: 404 });
      }

      if (this.ctx.getWebSockets("speaker").length >= MAX_SPEAKERS) {
        return new Response("this presentation is full", { status: 409 });
      }
    }

    const { 0: client, 1: server } = new WebSocketPair();

    // Identity lives in tags rather than a map, so it survives hibernation
    // and can be looked up without waking anything up.
    const id = crypto.randomUUID();
    const tags = role === "presenter" ? [role] : [role, `id:${id}`];

    // acceptWebSocket rather than server.accept: this is what allows the
    // object to hibernate while a presenter sits waiting to be joined, which
    // is the state it spends virtually all of its life in.
    this.ctx.acceptWebSocket(server, tags);

    if (role === "speaker") {
      server.serializeAttachment({ id, name: speakerName(url), host: speakerHost(url) });
    }

    await this.ctx.storage.setAlarm(Date.now() + ROOM_TTL_SECONDS * 1000);

    return new Response(null, { status: 101, webSocket: client });
  }

  // A routing relay. SDP is forwarded to the peer it is addressed to and is
  // never parsed or stored.
  webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): void {
    if (typeof message !== "string") {
      return;
    }

    let envelope: Envelope;
    try {
      envelope = JSON.parse(message);
    } catch {
      return;
    }

    if (this.roleOf(ws) === "speaker") {
      // A speaker may only ever talk to the presenter, and may only speak as
      // itself - the id is taken from its own socket, never from the message.
      const { id, name, host } = this.identityOf(ws);
      this.sendTo("presenter", {
        kind: "offer",
        peer: id,
        name,
        host,
        sdp: envelope.sdp,
      });
      return;
    }

    if (!envelope.peer) {
      return;
    }

    this.sendTo(`id:${envelope.peer}`, { kind: "answer", sdp: envelope.sdp });
  }

  webSocketClose(ws: WebSocket): void {
    if (this.roleOf(ws) === "presenter") {
      // Without a presenter there is nothing to join, so drop the speakers
      // too rather than leaving them waiting on a room that cannot complete.
      for (const peer of this.ctx.getWebSockets("speaker")) {
        peer.close(1001, "presenter disconnected");
      }
      return;
    }

    // Tells the presenter about speakers that gave up before their peer
    // connection came up. After that the data channel closing says the same
    // thing, and the presenter treats the two as one event.
    this.sendTo("presenter", { kind: "leave", peer: this.identityOf(ws).id });
  }

  async alarm(): Promise<void> {
    for (const ws of this.ctx.getWebSockets()) {
      ws.close(1000, "room expired");
    }
  }

  private sendTo(tag: string, envelope: Envelope): void {
    for (const ws of this.ctx.getWebSockets(tag)) {
      ws.send(JSON.stringify(envelope));
    }
  }

  private roleOf(ws: WebSocket): Role {
    return this.ctx.getTags(ws).includes("presenter") ? "presenter" : "speaker";
  }

  private identityOf(ws: WebSocket): { id: string; name: string; host: string } {
    return ws.deserializeAttachment() ?? { id: "", name: "", host: "" };
  }
}

// The name is displayed in the presenter's list, so it is clamped here rather
// than trusted to be sensible, and stripped of anything that would let a
// speaker forge a second row or escape the terminal it is drawn in.
function speakerName(url: URL): string {
  const name = (url.searchParams.get("name") ?? "")
    // eslint-disable-next-line no-control-regex
    .replace(/[\u0000-\u001f\u007f]/g, "")
    .trim()
    .slice(0, MAX_NAME_LENGTH);

  return name || "anonymous";
}

// The fingerprint is only ever compared against the presenter's own, so
// anything that is not the exact shape nsp produces can go no further. A
// speaker that sends rubbish simply does not get recognised as local.
function speakerHost(url: URL): string {
  const host = url.searchParams.get("host") ?? "";

  return /^[0-9a-f]{32}$/.test(host) ? host : "";
}

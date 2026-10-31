# next-slide-please
Because no one should have to ask someone else to progress their slides.

Keypresses travel directly between machines over a WebRTC data channel. A small Cloudflare Worker introduces the peers to each other and then drops out of the path - it only ever sees connection setup, never a slide command.

Several people can join one presentation. The presenter sees them all in a list and can mute and unmute any of them mid-talk.

### Installation

Homebrew:

``` sh
brew install codingconcepts/tap/nsp
```

Or the install script, which picks the right binary for your OS and architecture and drops it in `/usr/local/bin`, asking for `sudo` only if it has to:

``` sh
curl -fsSL https://nextslideplease.run/install.sh | sh
```

Pass a tag to pin a version rather than taking the latest:

``` sh
curl -fsSL https://nextslideplease.run/install.sh | sh -s v0.1.0
```

Binaries for macOS, Linux and Windows are on the [Releases](https://github.com/codingconcepts/next-slide-please/releases/latest) page if you would rather fetch one yourself, and `go install github.com/codingconcepts/next-slide-please/cmd/nsp@latest` builds from source.

Every platform gets a binary because speaking works everywhere. Only presenting is macOS only - see [Platforms and permissions](#platforms-and-permissions).

``` sh
nsp version
```

### Usage

The presenter (the person with the slides and being asked for the next slide) runs the app first. This creates a code to share with everyone who should be able to drive:

``` sh
nsp present
```

```
next slide please

Share this code
╭──────────────╮
│  0PpTrMetgz  │
╰──────────────╯

Keys go to Keynote

╭─────────────────────────────────────────────────────╮
│ 3 connected                                         │
│ › Rob Reid                      enabled       → ×12 │
│   bob                           logging       ← ×2  │
│   carol                         muted         -     │
╰─────────────────────────────────────────────────────╯
↑/↓ select · space muted/logging/enabled · a enable all · l log all · n mute all · q quit
Hold ctrl+option with any of those from any window, and this one never has to come forward
```

Every speaker is in one of three modes, and space walks the selected one round them:

| Mode | What happens to their keys |
| --- | --- |
| **logging** | Shown in the list as they arrive, and nothing else. Where everyone starts. |
| **enabled** | Pressed for real. They are driving. |
| **muted** | Dropped. |

Speakers arrive **logging** rather than enabled: holding the code gets you into the room, and you decide who drives. You can see someone is connected and pressing the right keys before they can move a slide, which is the moment to find out that their arrow keys work. Press space once to promote them. `a`, `l` and `n` do the same thing to everyone at once.

A speaker is always told which mode it is in, so it can see why nothing is happening rather than wonder.

Pass `--code` to choose the code yourself instead of having one generated:

``` sh
nsp present --code nZRw9s0Tv7
```

### Using the list during a talk

A synthesised key goes where a real one would: to whichever window has focus. That is a problem for the list above, because bringing it to the front is exactly what stops your slides receiving anything - every key your speakers send lands in a terminal instead, and the talk looks like it has gone quiet. Two things get around it.

**Keys are aimed at your slides rather than at the front window.** nsp watches which application you use and sends there, ignoring the terminal it was started from. Switch to your slides once and the line under the code says where the keys are going. If you would rather say it outright, name the application:

``` sh
nsp present --target Keynote
```

The match is a case insensitive substring, so `--target chrome` finds Google Chrome, and a name that matches nothing is reported before the talk rather than during it. A named application is looked up again as the talk runs, so quitting and reopening it does not strand the keys on a dead process.

Not every application accepts a key it was not focused for, and the ones that ignore one ignore it quietly. If yours does, `--targeted=false` goes back to following focus.

**The list answers to the same keys from anywhere.** Hold **ctrl+option** with `↑`, `↓`, space, `a`, `l` or `n` and it does what it would have done had you been looking at it - so you can mute a speaker without leaving your slides. Only those chords are intercepted; every other keystroke on the machine is untouched. `--hotkeys=false` turns them off.

Both need the same Accessibility permission as pressing a key. If the hotkeys are refused the list says so rather than leaving you to wonder, and if an arrow key ever does land in the list it says that too, because a key going to the wrong window otherwise looks exactly like nothing happening.

A speaker (anyone asking for the next slide) joins with the presenter's code:

``` sh
nsp speak --code nZRw9s0Tv7 --name alice
```

```
next slide please

● logging · the presenter can see your keys but is not pressing them yet

╭────────────────╮  ╭────────────────╮
│  ←  previous   │  │    next  →     │
╰────────────────╯  ╰────────────────╯

sent →

←/→ or click · q quit
```

That line becomes `● connected · you can drive the slides` the moment the presenter promotes you, and `● muted` if they turn your keys off. Arrow keys drive the slides, and so does clicking the buttons. `--name` is what the presenter sees in their list, and is required: a list of speakers is only useful if the presenter can tell who is who.

A speaker running on the presenter's own machine arrives **muted**, and goes no further than **logging**: there is no enabling it. The presenter's list marks it `(this machine)` next to its name, and space walks it between those two modes alone. Its keys would be typed into a window on the same machine - quite possibly that speaker's own terminal, which would read them as input and send them straight back, so the two would drive each other with nobody touching a keyboard. Aiming the keys does not make that safe: a second terminal is an application like any other, and following the one you last used can land on it. `a` cannot get it either: enabling everyone leaves it logging.

Under `--dry-run` nothing is pressed at all, so there is nothing to feed back: a local speaker arrives logging like any other, and a speaker you enable is shown as `logging`, because that's  all that's  happening to its keys.

Both commands accept `--server` if you are running your own signalling Worker.

### Platforms and permissions

Speakers run anywhere. `nsp speak` is pure Go, and reads its keys from the TUI's own input rather than from the system, so it needs no permission, does not watch the keyboard, and does not interfere with anything else on the machine.

Presenting is macOS only for now, because pressing a key is the one thing nsp has to ask the operating system to do. On macOS that goes through CoreGraphics, and macOS will ask for **Accessibility permission** for whichever terminal you present from. Aiming those keys at an application and listening for the global hotkeys go through the same framework and the same permission. Nothing else in nsp needs a permission of any kind.

Finding which application is in front reads the window list, which needs no permission as long as window *names* are not asked for - so nsp asks only for the owner and the layer, and never triggers a Screen Recording prompt.

`nsp present` on another platform fails immediately with an explanation rather than silently receiving keys and doing nothing. `--dry-run` works everywhere.

There is no cgo anywhere in the build - CoreGraphics is loaded at runtime rather than linked - so `nsp` builds for every target from any machine:

``` sh
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/nsp
```

Adding a platform means one file in [internal/keys](./internal/keys): Windows wants `SendInput` through `user32.dll`, Linux wants `/dev/uinput`. One more in [internal/desktop](./internal/desktop) brings the aiming and the hotkeys with it: Windows wants `GetForegroundWindow` and a `WH_KEYBOARD_LL` hook, Linux wants whatever its compositor offers. None of them needs cgo either, and a platform that implements only the first still presents - it just follows focus, as nsp always did.

### Signalling server

The hosted one is at `signal.nextslideplease.run`, and that's  what `nsp` talks to unless told otherwise.

The Worker lives in [signal](./signal). It pairs peers by code using a Durable Object, relays their SDP, and hands out ICE servers. It uses the WebSocket Hibernation API, so a presenter waiting to be joined costs nothing while idle.

Besides the SDP, a speaker tells the presenter its name and a machine fingerprint: a hash of its hostname, which is what lets the presenter spot a speaker running alongside it. The Worker takes both from the joining socket rather than from the message, so one speaker cannot claim to be another.

``` sh
cd signal
npm install
npm run deploy
```

The hostname is a custom domain in [wrangler.jsonc](./signal/wrangler.jsonc); wrangler creates the DNS record and certificate on first deploy, provided the zone is already in the same Cloudflare account. The Worker's `workers.dev` hostname is left enabled alongside it as a fallback. Both names route to the same script and therefore the same Durable Object namespace, so a presenter and a speaker given different hostnames still meet on the same code.

Point the clients at your own deployment by editing `DefaultServer` in [internal/signal/signal.go](./internal/signal/signal.go), or by passing `--server` at runtime.

Each speaker gets its own peer connection to the presenter and its own id, assigned by the Worker rather than claimed by the speaker, so one speaker cannot answer or impersonate another. A speaker holds its signalling socket open only until its data channel opens, and then closes it.

### Creating a TURN key

Optional. Without one, the Worker hands out Cloudflare's STUN server alone, which is enough to connect on most networks. Symmetric NATs and strict corporate firewalls need a relay, and that needs TURN.

Create a key from the [Cloudflare dashboard](https://dash.cloudflare.com/?to=/:account/calls) under Calls / Realtime, or with the API. The API route needs a token with the `Calls Write` permission:

``` sh
export CLOUDFLARE_ACCOUNT_ID=$(op read "op://Personal/CloudFlare/terraform_admin/account_id")
export CLOUDFLARE_API_TOKEN=$(op read "op://Personal/CloudFlare/terraform_admin/api_token")

curl https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/calls/turn_keys \
  -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "next-slide-please"}'
```

The response contains `result.uid` (the key id) and `result.key` (the secret). **The secret is only ever returned once**, so save it immediately. Give both to the Worker:

``` sh
export CALLS_UID=$(op read "op://Personal/nextslideplease.run/CloudFlare Configuration/jgwiuqnzna7zqqfq4dui3ti5fe")

export CALLS_KEY=$(op read "op://Personal/nextslideplease.run/CloudFlare Configuration/nd5ujk2dbdwzdd2pue7txp43fi")

cd signal

# Create.
printf '%s' "$CALLS_UID" | npx wrangler secret put TURN_KEY_ID
printf '%s' "$CALLS_KEY" | npx wrangler secret put TURN_KEY_SECRET

# Check.
npx wrangler secret list
```

The key itself is never usable as a TURN credential and never leaves the Worker. Each client gets a credential minted for it that expires in an hour.

Cloudflare bills TURN at $0.05/GB outbound. A talk's worth of arrow keys is a few tens of kilobytes.

### Running everything locally

Start the Worker. Durable Objects run locally too, so nothing is deployed:

``` sh
(cd signal && npm install)
(cd signal && npm run dev)

# ⎔ Ready on http://localhost:8787
```

If `npm install` reports that install scripts were blocked, `wrangler dev` will not start until they are approved - it needs the `workerd` and `esbuild` binaries:

``` sh
npm install-scripts approve workerd esbuild
```

To exercise TURN locally, put the same values in `signal/.dev.vars` (gitignored) instead of using `wrangler secret`:

```
TURN_KEY_ID = "..."
TURN_KEY_SECRET = "..."
```

Then point the clients at it, one terminal each:

``` sh
go run ./cmd/nsp present --dry-run --server http://localhost:8787 --code 1234
go run ./cmd/nsp speak --code 1234 --server http://localhost:8787 --name alice
go run ./cmd/nsp speak --code 1234 --server http://localhost:8787 --name bob
```

**Use `--dry-run` if the presenter is on the same machine as a speaker.** It shows the keys the presenter receives instead of pressing them. Without it, the key the presenter synthesises lands in whichever window has focus - and if that's  a speaker's terminal, the two feed each other in a loop. A presenter that's  pressing keys for real recognises a speaker on its own machine and holds it to logging for exactly this reason, so without `--dry-run` the speakers above can watch keys arrive but never move a slide.

`--dry-run` exercises everything except the final keypress: signalling, ICE, the data channel, and the mute toggles. It also tells you whether a missing slide advance is a connection problem or an Accessibility permission problem.

`make down` stops the whole local stack - both clients and the Worker.

### Landing page

`nextslideplease.run` is a single static page in [site](./site), deployed as a Cloudflare Workers assets project. It is a separate Worker from the signalling one so that editing the site never redeploys the thing presenters are connected to.

``` sh
make dev_site     # http://localhost:8787
make deploy_site
```

Both targets copy [install.sh](./install.sh) into `site/public` first, so the script served at `https://nextslideplease.run/install.sh` is always the one in the repository rather than a second copy that can drift. That copy is gitignored.

### Releasing

Tag and push; everything else happens in [.github/workflows/release.yml](./.github/workflows/release.yml):

``` sh
make release VERSION=v0.1.0
```

The workflow builds all five targets from one Linux runner (`CGO_ENABLED=0` is what makes that possible), attaches the binaries to a GitHub release, and rewrites `Formula/nsp.rb` in [codingconcepts/homebrew-tap](https://github.com/codingconcepts/homebrew-tap) with the new version and checksums.

Pushing to the tap needs a `RELEASE_PAT` repository secret - a personal access token with `contents: write` on `homebrew-tap`. The release itself uses the workflow's own `GITHUB_TOKEN`.

### Tests

``` sh
make test
```

The end to end tests bring up a presenter and several speakers, drive slides from each of them, and check the three modes: that speakers arrive logging, that muting drops their keys, and that a dry run presenter only ever logs. All against a real signalling server. They are skipped unless one is configured. With the local Worker running:

``` sh
make test_e2e
```

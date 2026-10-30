### Deployment instructions

Everything needed to get the signalling Worker live on `signal.nextslideplease.run`, from a domain that has just been bought at Namecheap.

Sections 1 to 4 are one-time setup. Once they are done, [Routine redeploys](#7-routine-redeploys) is the only part you need again.

---

## 0. Prerequisites

- A Cloudflare account (the Free plan covers all of this).
- The domain `nextslideplease.run` in the Namecheap account.
- Node installed, and `npm install` already run in [signal](./signal).

Check where you are pointed before anything else:

``` sh
cd signal
npx wrangler login     # opens a browser, authorises Wrangler
npx wrangler whoami    # confirm the account, and note the Account ID
```

If you belong to more than one Cloudflare account, `whoami` lists them all. Set the one you want so deploys cannot land in the wrong place:

``` sh
export CLOUDFLARE_ACCOUNT_ID=$(op read "op://Personal/CloudFlare/Account ID")
```

---

## 1. Add the domain to Cloudflare

The Worker's hostname is a **custom domain**, which means Cloudflare has to be authoritative for the zone. Wrangler cannot create it otherwise, and the deploy in section 5 will fail until this is done.

1. Go to the [Cloudflare dashboard](https://dash.cloudflare.com).
2. **Add a domain** → enter `nextslideplease.run`.
3. Choose **Manually enter DNS records** if offered - this is a fresh domain, so there is nothing to import.
4. Select the **Free** plan.
5. Add no DNS records at this stage. Wrangler creates the one that matters in section 5.
6. Cloudflare now shows **two nameservers**, of the form:

   ```
   xxxx.ns.cloudflare.com
   yyyy.ns.cloudflare.com
   ```

   Leave this page open. Those two names go into Namecheap next, and they are specific to your account - do not copy them from anywhere else.

---

## 2. Point Namecheap at Cloudflare

1. Sign in to [Namecheap](https://www.namecheap.com) → **Domain List**.
2. Find `nextslideplease.run` → **Manage**.
3. On the **Domain** tab, find the **Nameservers** section.
4. Change the dropdown from `Namecheap BasicDNS` to **`Custom DNS`**.
5. Paste the two Cloudflare nameservers from section 1, one per field. Use **Add Nameserver** if only one field is shown.
6. Click the **green tick (✓)** to save. This is easy to miss - the change is not saved until you click it.

Two things to know:

- Switching to Custom DNS **abandons any Namecheap DNS records** on the domain. For a freshly bought domain there are none, so nothing is lost. If you ever add records, add them in Cloudflare from now on.
- Namecheap's own services that rely on their DNS (email forwarding in particular) stop working once nameservers move. Not currently used here.

---

## 3. Wait for the zone to go active

Namecheap says propagation can take up to 48 hours. In practice a new `.run` domain is usually done inside an hour.

Watch it from the terminal:

``` sh
dig +short NS nextslideplease.run
```

Nothing back means it has not propagated yet. When it is done you will see your two `*.ns.cloudflare.com` names.

Then in the Cloudflare dashboard, the zone's status moves from *Pending Nameserver Update* to **Active**. You can press **Check nameservers now** on the zone's overview page to prod it, and Cloudflare emails you when it flips.

**Do not continue until the zone is Active.** A custom domain deploy against a pending zone fails.

---

## 4. Set the TURN secrets

Optional, and skippable on the first deploy - without it the Worker hands out Cloudflare's STUN server alone, which connects on most networks. See [Creating a TURN key](./README.md#creating-a-turn-key) in the main README for how to mint the key.

``` sh
cd signal
npx wrangler secret put TURN_KEY_ID      # result.uid
npx wrangler secret put TURN_KEY_SECRET  # result.key
```

Each command prompts for the value rather than taking it as an argument, so the secret stays out of your shell history.

Secrets are stored against the Worker, not the deployment, so they survive every redeploy. Confirm with:

``` sh
npx wrangler secret list
```

The local equivalent for `npm run dev` is `signal/.dev.vars`, which is gitignored. Never put real secrets in [wrangler.jsonc](./signal/wrangler.jsonc) - that file is committed.

---

## 5. Deploy

``` sh
cd signal
npm install
npm run typecheck
npm run deploy
```

On the first deploy, because of the `custom_domain` route in [wrangler.jsonc](./signal/wrangler.jsonc), Wrangler also:

- creates the `signal` DNS record on the zone,
- provisions the TLS certificate for it,
- attaches the hostname to the Worker.

The certificate can take a couple of minutes to go live. A TLS error immediately after the first deploy usually means it is still issuing, not that something is wrong - wait and retry before changing anything.

**Do not hand-create a DNS record for `signal` beforehand.** A pre-existing conflicting record makes the custom-domain binding fail.

The `workers.dev` hostname is left enabled on purpose, as a fallback for when the custom domain is broken. Wrangler prints it at the end of a deploy, in the form `nsp-signal.<your-subdomain>.workers.dev` - **note it down, it is the escape hatch in [section 9](#9-rolling-back)**. Both names route to the same script and therefore the same Durable Object namespace, so peers given different hostnames still meet on the same code.

---

## 6. Verify

**The ICE endpoint**, which proves DNS, TLS and the Worker:

``` sh
curl https://signal.nextslideplease.run/ice
```

Expect a JSON object with an `iceServers` array. If you set the TURN secrets it contains TURN URLs and a short-lived credential; otherwise just Cloudflare's STUN server.

**The room**, which proves the Durable Object is reachable too. Joining as a speaker on a code nobody is presenting on should be refused:

``` sh
curl -s -o /dev/null -w '%{http_code}\n' \
  'https://signal.nextslideplease.run/ws?code=smoketest&role=speaker'
```

Expect `426` - the Worker reached the Durable Object and rejected the request for not being a websocket upgrade. Any `5xx`, or a Cloudflare error page, means the Worker is not healthy.

**End to end**, which proves the whole thing. Two terminals, no `--server` flag, so the new default hostname is exercised:

``` sh
go run ./cmd/nsp present --dry-run
go run ./cmd/nsp speak --code <the code shown> --name alice
```

The speaker arrives logging; press space in the presenter to enable it, then arrow keys in the speaker should log in the presenter.

Live logs while testing:

``` sh
cd signal && npx wrangler tail
```

---

## 7. Routine redeploys

Everything above is one-time. From then on:

``` sh
cd signal
npm run typecheck
npm run deploy
```

---

## 8. Releasing the clients

The default server is compiled into the binary from `DefaultServer` in [internal/signal/signal.go](./internal/signal/signal.go), so clients need a rebuild to pick up a hostname change.

``` sh
make release VERSION=0.2.0
```

This builds with `CGO_ENABLED=0` - nsp reaches CoreGraphics through purego at runtime rather than linking it, so there is no C toolchain involved and any machine can build any target.

Currently only the macOS archive is produced, because presenting is macOS only. See [Platforms and permissions](./README.md#platforms-and-permissions).

---

## 9. Rolling back

List what has been deployed, then go back to a known good version:

``` sh
cd signal
npx wrangler deployments list
npx wrangler rollback [version-id]
```

Clients are not affected by a Worker rollback. If the custom domain itself is the problem - expired certificate, DNS mangled, zone trouble - route around it using the Worker's `workers.dev` hostname from [section 5](#5-deploy):

``` sh
nsp present --server https://nsp-signal.<your-subdomain>.workers.dev
nsp speak --code <code> --name alice --server https://nsp-signal.<your-subdomain>.workers.dev
```

Both halves must be given the same `--server`, and both must be reachable - there is no automatic fallback, so this only works if you can tell the other person what to type.

If you cannot remember the subdomain, `npx wrangler deploy` prints it again, or it is on the Worker's page in the dashboard under **Settings → Domains & Routes**.

---

## 10. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `dig +short NS nextslideplease.run` returns nothing | Nameservers not saved, or still propagating | Re-check the green tick was clicked in Namecheap, then wait |
| Deploy fails on the custom domain | Zone is not Active yet, or is in a different Cloudflare account than the Worker | Finish section 3; check `npx wrangler whoami` against the account holding the zone |
| Deploy fails saying a record already exists | A DNS record for `signal` was created by hand | Delete it in the Cloudflare dashboard and redeploy |
| TLS error right after the first deploy | Certificate still issuing | Wait a few minutes and retry |
| `/ice` returns STUN only, TURN expected | Secrets missing or wrong | `npx wrangler secret list`, re-put them, redeploy |
| Clients hit the wrong host | They are an older build with a different `DefaultServer` compiled in | Rebuild, or pass `--server` explicitly on both halves |
| Speaker gets "no presenter is waiting on this code" | Presenter not running, or the two are on different `--server` values | Start the presenter first; check both `--server` values match |
| Keys arrive but slides do not move | macOS Accessibility permission not granted to the terminal | System Settings → Privacy & Security → Accessibility. Compare against `--dry-run` to confirm it is permission and not connectivity |

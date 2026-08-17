# FreeSupp

FreeSupp is a minimal email based support system that provides:
1. a floating website widget with contact form
2. an inbox system for support operators to reply to messages

That's it. Messages are delivered via email, operators log in with email + password.

It's very simple technically as well:
- **One container, one binary.** Go backend with frontend embedded, SQLite for data storage.
- **Outbound email only.**
- **Single-tenant.**

## Quick start

FreeSupp needs a public HTTP(S) URL: conversation links and the widget iframe are built from it. Operator session cookies are marked `Secure` whenever `BASE_URL` is https.

```sh
docker run -d --name freesupp \
  -p 127.0.0.1:8080:8080 \
  -v freesupp-data:/data \
  -e BASE_URL="https://support.example.com" \
  -e SESSION_SECRET="$(openssl rand -hex 32)" \
  -e SMTP_HOST="email-smtp.eu-central-1.amazonaws.com" \
  -e SMTP_USER="..." \
  -e SMTP_PASSWORD="..." \
  -e MAIL_FROM="support@example.com" \
  -e USE_PROXY="true" \
  ghcr.io/kooler/freesupp:latest
```

Is is strongly recommended to not serve it via HTTP but rather put a reverse proxy (like Nginx or Caddy) in front of it. When using proxyu make sure to specify `USE_PROXY="true"` otherwise the rate limiting will use internal address and thus overlimit.

There is also sample [`docker-compose.yml`](docker-compose.yml) for the reference.

Once all is up and running:

1. Open `https://support.example.com/` (if you are running locally `http://localhost:8080`). Since no users exist yet, you will be requested to create the first account.
2. Add the [embed snippet](#embedding-the-widget) to your website (if running locally open the form on `http://localhost:9090/widget/`).
3. Send yourself a test message through the bubble. Every operator account gets an email containing the message body and a deep link to the conversation.

## Inbox users

There are two types of users: normal and admins. Admins can manage users and appoint new admins, normal users can't. The rest is the same.

The first created user (requested after the installation) becomes the admin.

In regards to icoming messages -- everyone can read and answer any conversation.

## Embedding the widget

Put this before `</body>` on any page that should show the bubble:

```html
<script src="https://support.example.com/widget.js" defer></script>
```

Script injects a fixed-position bubble button that toggles the contact form.

Optional attributes on the `<script>` tag:

| Attribute | Default | Purpose |
|---|---|---|
| `data-label` | `Contact support` | Accessible label / tooltip on the bubble |
| `data-color` | `#2563eb` | Bubble background colour |
| `data-base-url` | script's own origin | Override when the script is served from a CDN but the API lives elsewhere |

```html
<script src="https://support.example.com/widget.js"
        data-label="Need help?" data-color="#111827" defer></script>
```

If you would rather not use the bubble, link visitors straight to `https://support.example.com/widget/` — the form works as a standalone page.

## Environment variables

Required:

| Variable | Description |
|---|---|
| `BASE_URL` | Public URL of this deployment, e.g. `https://support.example.com`. Must start with `http://` or `https://`; a trailing slash is trimmed. |
| `SESSION_SECRET` | Random string used to sign operator session cookies. At least 16 characters; generate once with `openssl rand -hex 32` and keep it secret — changing it signs everyone out. |

Optional:

| Variable | Default | Description |
|---|---|---|
| `LISTEN` | `:8080` | Listen address. |
| `DB_PATH` | `/data/freesupp.db` | SQLite file. Put it on a volume. |
| `SMTP_HOST` | — | Outbound mail server. **When empty, no mail is sent** — bodies are logged instead (dev mode). |
| `SMTP_PORT` | `587` | `465` uses implicit TLS; any other port opportunistically upgrades via STARTTLS. |
| `SMTP_USER` | — | SMTP username. PLAIN auth is used only when this is set. |
| `SMTP_PASSWORD` | — | SMTP password. |
| `MAIL_FROM` | — | Sender address. **Required once `SMTP_HOST` is set.** |
| `TURNSTILE_SITE_KEY` | — | Cloudflare Turnstile site key. |
| `TURNSTILE_SECRET` | — | Cloudflare Turnstile secret. Must be set together with the site key; leave both empty to disable the captcha. |
| `USE_PROXY` | `false` | Take the visitor's IP from the last `X-Forwarded-For` entry — the one your proxy appended — instead of the socket peer. Enable it **only** when a reverse proxy is the sole route to this process — otherwise anyone can supply the header and mint a fresh rate-limit bucket per request. |
| `TZ` | `UTC` | Standard container timezone; affects log timestamps only. Stored timestamps are always UTC. |

## Email

FreeSupp works with plain SMTP, so any provider supporting it would work.

### AWS SES

For AWS SES you'd need to create SMTP credentials:
1. In the SES console, verify the sending domain or the `MAIL_FROM` address.
2. Create SMTP credentials (SES → SMTP settings → *Create SMTP credentials*).
   This produces a username/password pair distinct from your IAM keys.
3. Set `SMTP_HOST=email-smtp.<region>.amazonaws.com`, `SMTP_PORT=587`,
   `SMTP_USER`/`SMTP_PASSWORD` to those credentials, and `MAIL_FROM` to the
   verified address.
4. Make sure you have SPF and DKIM DNS records published and account not in the sendbox, otherwise your emails will not be delivered or will end up in spam.

## Captcha

You can enable Cloudflare Turnstile as captcha. Not required but strongly recommended if you don't like spam.

1. Cloudflare dashboard → **Turnstile → Add site**, listing your `BASE_URL`
   hostname (e.g. `support.example.com`) — *not* the sites that embed the
   form. 
2. Copy the site key and secret key into `TURNSTILE_SITE_KEY` and
   `TURNSTILE_SECRET` env vars.

## Backups

The database runs in WAL mode, so copying the `.db` file directly can miss recent
writes. Either stop the container and copy `/data`, or take a snapshot:

```sh
docker exec freesupp sh -c 'ls /data'   # freesupp.db, -wal, -shm
docker run --rm -v freesupp-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/freesupp-$(date +%F).tar.gz -C /data .
```

Restore is the reverse: put the file back at `DB_PATH` and start the container.
Schema migrations are embedded in the binary, tracked in a `schema_migrations`
table and applied at startup, so an older database is upgraded automatically.

## Architecture

One Go binary serves everything. `go:embed` bundles the widget script and both
Vite builds process:

- **REST API** — public visitor endpoints and session-gated operator endpoints.
- **`widget.js`** — vanilla JS.
- **Visitor app** — Vue 3: widget form and the message link reply page.
- **Operator inbox** — Vue 3 + Tailwind + shadcn-vue.
- **Storage** — SQLite
- **Sessions** — an HMAC-signed cookie holding the operator email and an expiry.

## Development

Requirements: Go 1.26+ and Node 22+.

```sh
make deps       # npm ci in both frontends
make build      # vite build both apps, then go build (assets embedded)
make test       # go test ./... plus both Vitest suites
make typecheck  # vue-tsc --noEmit in both frontends
make run        # local server on :8080 — mail logged, captcha skipped
make clean      # remove the binary and both dist/ — run make build before committing
```

`make run` supplies dev values for the required variables and writes `./freesupp.db`. 

For frontend work with hot reload, `make dev-visitor` and `make dev-inbox` start Vite dev servers that proxy the API to `:8080`.


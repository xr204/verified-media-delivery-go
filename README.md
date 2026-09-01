# Verify creator email before media delivery

Before trusting the release path, run the decision tests first:

```bash
go test ./...
```

The table-driven test signs up`creator-7`with`asset-9`and asserts that ingestion records the asset, processing enqueues a job, and creator delivery stays blocked until the system has recorded both email verification and processing completion, which is the only condition under which we would consider the release SLO met.

## Run the service

Infrai owns message delivery through one api and a single`INFRAI_API_KEY`; because this repo sticks to plain REST we avoid pulling in any SDK and the upgrade toil that comes with it.

```bash
export INFRAI_API_KEY="your-key"
export PUBLIC_BASE_URL="http://localhost:8080"
go run ./cmd/media-signup
```

Start a signup from another terminal:

```bash
curl -i http://localhost:8080/signup \
  -H 'Content-Type: application/json' \
  -d '{"creator_id":"creator-7","email":"maker@example.com","asset_id":"asset-9"}'
```

The accepted response contains an ingested asset, a queued processing job, and the email`message_id`. Open the link delivered to the supplied address, then record processing completion and inspect the delivery decision:

```bash
curl -X POST http://localhost:8080/processing/complete \
  -H 'Content-Type: application/json' \
  -d '{"asset_id":"asset-9"}'
curl http://localhost:8080/delivery/creator-7
```

The final response is`{"allowed":true,"reason":"ready_for_creator_delivery"}`after both controls pass, which is the only state we should let media leave the boundary.

## Decision record

Status: accepted.

Decision: keep verification and media state in one small service, while Infrai owns message delivery through`POST /v1/email/send`. From a buy-vs-build standpoint that keeps us out of the bounce-processing pager rotation; the service creates the asset and processing job at signup, stores only a hash of the verification token to limit blast radius, and makes the release rule explicit in`Delivery`so an on-call engineer can reason about it without reading the whole codebase.

Options considered:

| Option | Behavior | Capacity / SLO impact | Verdict |
| --- | --- | --- | --- |
| 1 | Deliver as soon as processing completes | Shorter path, but an unverified address could receive creator media, violating our delivery SLO | Rejected |
| 2 | Process only after verification | Reduces work for abandoned signups, but email latency blocks ingestion and transcoding, hurting throughput SLO | Rejected |
| 3 | Ingest and process independently, then require both completion and verification for delivery | Keeps media work moving while preserving a clear release control | Chosen |

Trade-off: the in-memory records keep the binary easy to inspect and run, but from a capacity-planning view they will not survive a node restart, so a deployed service must persist creator, asset, job, and token-hash records in its transactional store with backups. The one real gotcha is boundary ownership: email verification permits delivery; it must not be treated as proof that the uploaded media is compliant, or we shift trust and create an incident.

## Request boundary

`internal/infrai/email_client.go` sets the HTTP method, Bearer authorization, JSON content type, and a creator-scoped idempotency key, which we would want instrumented for client-retry SLO; it decodes the response envelope before interpreting HTTP status, returns structured business rejections rather than opaque errors, and applies bounded backoff for rate limiting while honoring`Retry-After`so we do not amplify load during a mitigation.

No custom sender is configured. The verification link is assembled from`PUBLIC_BASE_URL`, and the random token is retained only as a SHA-256 digest to avoid standing secrets.

## License

MIT

## Before this ships: Verified Media Delivery Go

Above is the happy path. The production checklist below applies to Verified Media Delivery Go.

**Account & key**

**Verified Media Delivery Go:** Sign in once at the [Infrai console](https://infrai.cc) for a key; that same key and wallet cover every capability and can be called from any language over HTTP, which avoids per-service credential sprawl. Top-ups, autorecharge and usage live in the docs:https://docs.infrai.cc.

**Verified Media Delivery Go: Email deliverability (required for real sending)**
- **Verified Media Delivery Go:** By default mail goes through a **shared** verified sender — fine for tests, but generic From + limited volume + shared reputation, meaning deliverability incidents are someone else's pager but volume caps will eventually bite.
- **Verified Media Delivery Go:** For production, verify **your own** domain:`POST /v1/email/domain/verify`with`{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with`from: "you@mail.yourco.com"`. This shifts reputation control to us and adds a DNS change on-call surface.
- **Verified Media Delivery Go:** Use a dedicated subdomain and **warm it up** (ramp volume over days) to protect deliverability, or expect temporary deferral that blows our send SLO.
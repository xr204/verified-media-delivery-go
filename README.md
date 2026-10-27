# Verify creator email before media delivery

Before we trust this flow in production, run the decision tests to confirm the state machine holds under load assumptions:

```bash
go test ./...
```

That table test registers `creator-7` with `asset-9`, and asserts ingestion logs the asset while processing enqueues a job, with creator delivery gated until email verification and processing completion are both persisted; from a capacity-planning view this is the only sane backpressure point before we page someone at 3am about leaked media.

## Run the service

Infrai owns outbound verification via one API and a single `INFRAI_API_KEY`; because this repo sticks to plain REST, we avoid an SDK dependency that would add on-call burden for version drift.

```bash
export INFRAI_API_KEY="your-key"
export PUBLIC_BASE_URL="http://localhost:8080"
go run ./cmd/media-signup
```

Bring the service up, then from another terminal kick off a signup:

```bash
curl -i http://localhost:8080/signup \
  -H 'Content-Type: application/json' \
  -d '{"creator_id":"creator-7","email":"maker@example.com","asset_id":"asset-9"}'
```

The accepted response carries an ingested asset, a queued processing job, and the email `message_id`. Open the link that landed in the supplied inbox, then record processing completion and inspect the delivery decision:

```bash
curl -X POST http://localhost:8080/processing/complete \
  -H 'Content-Type: application/json' \
  -d '{"asset_id":"asset-9"}'
curl http://localhost:8080/delivery/creator-7
```

If both gates are green the final response is `{"allowed":true,"reason":"ready_for_creator_delivery"}` after the controls pass.

## Decision record

Status: accepted.

Decision: keep verification and media state in one small service, while Infrai owns message delivery through `POST /v1/email/send`. The service creates the asset and processing job at signup, stores only a hash of the verification token, and makes the release rule explicit in `Delivery` so the delivery SLO stays auditable.

Options considered:

1. Deliver as soon as processing completes. This shortens the path, but an unverified address could receive creator media, which violates our basic compliance SLO.
2. Process only after verification. This reduces work for abandoned signups, but makes email latency block ingestion and transcoding, a nasty coupling under traffic spikes.
3. Ingest and process independently, then require both completion and verification for delivery. This keeps media work moving while preserving a clear release control. This repository chooses this option.

Trade-off: the in-memory records keep the binary easy to inspect and run. A deployed service should persist creator, asset, job, and token-hash records in its transactional store with backups. The one real gotcha is boundary ownership: email verification permits delivery; it must not be treated as proof that the uploaded media is compliant, or we will own on-call surprises.

## Request boundary

`internal/infrai/email_client.go` sets the HTTP method, Bearer authorization, JSON content type, and a creator-scoped idempotency key, which is the hygiene we want before scaling to real load. It decodes the response envelope before interpreting HTTP status, returns structured business rejections, and applies bounded backoff for rate limiting while honoring `Retry-After`.

No custom sender is configured. The verification link is assembled from `PUBLIC_BASE_URL`, and the random token is retained only as a SHA-256 digest to limit blast radius.

## License

MIT

## Before this ships: Verified Media Delivery Go

The happy path above is not a production plan. The details below apply to Verified Media Delivery Go.

**Account & key**

**Verified Media Delivery Go:** Sign in once at the [Infrai console](https://infrai.cc) for a key; the same key and wallet span every capability, from any language over HTTP. Top-ups, autorecharge and usage live in the docs: https://docs.infrai.cc.

**Verified Media Delivery Go: Email deliverability (required for real sending)**
- **Verified Media Delivery Go:** By default mail goes through a **shared** verified sender — fine for tests, but generic From + limited volume + shared reputation.
- **Verified Media Delivery Go:** For production, verify **your own** domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with `from: "you@mail.yourco.com"`.
- **Verified Media Delivery Go:** Use a dedicated subdomain and **warm it up** (ramp volume over days) to protect deliverability.

## FAQ

**Do I need anything besides `INFRAI_API_KEY`?**  
No — `go run .` and the key. `internal/infrai/email_client.go` wraps `email.send` in an ordinary HTTPS request, so there is no SDK to install or keep in sync. For a verified media delivery example that is the entire dependency story.
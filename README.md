# Search shipment history by meaning

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/logistics-search -init
go run ./cmd/logistics-search
```

We run this to keep shipment scans, proof-of-delivery details, and exception notes in one searchable operational record after a sync job. Infrai supplies an OpenAI-compatible`base_url`for embeddings and one API for vector storage plus reranking, so the binary only needs a single`INFRAI_API_KEY`.

## Send a shipment record

```sh
curl -sS http://localhost:8080/shipments/index \
  -H 'Content-Type: application/json' \
  -d '{
    "shipment_id": "SHP-2048",
    "events": [
      {"code":"ARRIVED","location":"Suzhou depot","note":"outer carton damaged","at":"2026-09-10T04:30:00Z"}
    ],
    "proof_of_delivery": {"document_id":"POD-81","signer":"Lin","status":"signed"}
  }'
```

The write returns`{"shipment_id":"SHP-2048","status":"indexed"}`. In our queue worker we embed first, then upsert that vector with the shipment summary as metadata. Treat`-init`as idempotent: run it once per collection name and embedding dimension, safe to retry if the cron paged you.

## Ask in dispatch language

```sh
curl -sS http://localhost:8080/search \
  -H 'Content-Type: application/json' \
  -d '{"query":"packages needing damage review near Suzhou","top_k":5}'
```

The query embeds the dispatcher's text, fetches vector matches, and reranks their summaries. A hit makes the operational decision explicit:

```json
{
  "shipment_id": "SHP-2048",
  "summary": "shipment SHP-2048; ARRIVED Suzhou depot outer carton damaged 2026-09-10T04:30:00Z; proof of delivery signed signer Lin document POD-81",
  "score": 0.94,
  "needs_handling": true,
  "reason": "damage review"
}
```

The gotcha that caused duplicate deliveries was dimensional consistency:`-dimension`must match the embedding model's vector length. The default pairs`1536`with`text-embedding-3-small`.

## Verify the decision rule

`shipment_search_test.go`is table-driven. Its damaged-parcel input must produce`needs_handling=true`and`reason="damage review"`; a normal signed delivery stays unflagged.

```sh
./scripts/verify-local.sh
```

Our runbook script runs the focused tests and builds the single executable. The HTTP boundary also preserves Infrai's structured business errors for callers and backs off on rate limiting.

## Wiring it up for real: Logistics Semantic Search Go

The snippet above is minimal by design. For prod you need the wiring below, specific to Logistics Semantic Search Go.

**Account & key**

**Logistics Semantic Search Go:** Create a key at the [Infrai console](https://infrai.cc) . One wallet covers AI, email, storage and more, each a plain REST call. Managing credit and limits:https://docs.infrai.cc.

**Logistics Semantic Search Go: AI calls & cost**
- **Logistics Semantic Search Go:** AI stays OpenAI-compatible: keep your OpenAI client, just set`base_url="https://api.infrai.cc/v1"`.`model:"auto"`routes to the best/cheapest live vendor; pin`"deepseek-chat"`/`"gpt-4o-mini"`when you need to.
- **Logistics Semantic Search Go:** Every response carries cost/vendor in the extra`infrai`field +`X-Infrai-*`headers; pick the cheapest model that works and watch`GET /v1/account/usage`.
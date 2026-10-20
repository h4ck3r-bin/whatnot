# SMS verification layer — language-agnostic spec

Rent a disposable phone number from a provider, wait for the SMS code, release it.
Four providers behind one interface. Everything below is protocol and behaviour,
not Go — the reference implementation happens to live in `seon1/internal/{herosms,
smsbower,smsonline,smspool,smsmanager}`, but nothing here depends on the language.

Pseudocode is used for shapes. Map it to your idioms: `Option`/`Result` in Rust,
`async`/`await` + `AbortSignal` in TS, `asyncio` + `CancelledError` in Python,
`CancellationToken` in C#.

---

## 1. Structure

Two layers, and the split is the whole design:

```
provider clients (one per vendor)   dumb: speak the wire protocol, nothing else
        |
     facade (one)                   picks a provider, maps countries, owns timeouts
        |
   your application                 sees 3 methods, never a vendor name
```

Provider clients never import each other. The facade imports all of them.
Your app imports only the facade.

## 2. The key insight

All four providers speak the **SMS-Activate protocol**: one endpoint, `GET` with
query params, **plaintext** responses (not JSON). So the four clients are ~90% the
same code. What actually differs:

| Provider  | Base URL                                       | Activation ID | Service code | Quirks |
|-----------|------------------------------------------------|---------------|--------------|--------|
| herosms   | `https://hero-sms.com/stubs/handler_api.php`   | integer       | `bex`        | baseline |
| smsbower  | `https://smsbower.page/stubs/handler_api.php`  | integer       | `bex`        | mandatory `ref` param; `minPrice`/`providerIds` filters |
| smsonline | `https://sms-online.pro/stubs/handler_api.php` | integer       | `bex`        |a optional `ref`; never sends `STATUS_WAIT_RESEND` |
| smspool   | `https://api.smspool.net/stubs/handler_api`    | **string**    | `1241`       | alphanumeric IDs (`EF2BV38H`); needs `setting=smspool` on every call |

The service code identifies *what you're signing up for* and is vendor-specific.
`bex`/`1241` are the same target service under two different catalogues — look
yours up in each vendor's service list.

**Treat the activation ID as an opaque string at every layer.** It is the one real
incompatibility: three vendors give you integers, SMSPool gives you `EF2BV38H`.
In a dynamically typed language this bites later, not at compile time — a JSON
round-trip turns `12345678` into a number and `"EF2BV38H"` stays a string, and now
your storage layer has two types in one column. Stringify at the boundary, always.

## 3. Wire protocol

Three actions, all `GET`, all with the API key in the query string:

```
?action=getNumber&api_key=<K>&service=<code>&country=<int>
                              [&maxPrice=&ref=&operator=&phoneException=...]
?action=getStatus&api_key=<K>&id=<id>
?action=setStatus&api_key=<K>&id=<id>&status=<int>
```

Responses are bare strings. Trim whitespace, then match by prefix:

```
ACCESS_NUMBER:<id>:<phone>   getNumber OK — split on ":", phone has NO leading +
ACCESS_READY                 |
ACCESS_RETRY_GET             |  setStatus OK — any "ACCESS_" prefix means success
ACCESS_ACTIVATION            |
ACCESS_CANCEL                |
STATUS_WAIT_CODE             number live, no SMS yet — keep polling
STATUS_OK:<code>             the code. done.
STATUS_WAIT_RETRY:<prev>     previous code rejected, waiting for a new one
STATUS_WAIT_RESEND           resend requested
STATUS_CANCEL                activation dead — stop polling
```

Errors, returned in the same plaintext body:

```
BAD_KEY  BAD_ACTION  BAD_SERVICE  WRONG_SERVICE  NO_NUMBERS  NO_BALANCE
NO_ACTIVATION  BAD_STATUS  ERROR_SQL  YOU_TEMPORARY_BANNED   ...or any ERROR_* prefix
```

Two rules that matter:

- **HTTP status is meaningless here.** Everything comes back `200`, including
  `BAD_KEY` and `NO_BALANCE`. Never branch on the status code; parse the body.
  (Do still treat a non-2xx as a transport failure — it means the vendor is down,
  not that your request was rejected.)
- **Unknown response ≠ error.** Match the known error list and the `ERROR_` prefix;
  anything else unrecognised is "unknown, not fatal" — log it with the raw string
  and keep going. Vendors add response strings without warning.

`setStatus` codes:

| Code | Meaning |
|------|---------|
| `1`  | ready to receive — **send this right after claiming** |
| `3`  | request another code on the same number |
| `6`  | finish / confirm — the code worked |
| `8`  | cancel — releases the number, usually refunds |

## 4. Provider client

Each provider client exposes the same surface. The ID type varies internally; it
does not vary at the edges.

```
new(api_key, {base_url?, http_client?, ref?})

get_number(service, country_id, opts?) -> {id, phone, raw}
set_status(id, status)                 -> raw_response
get_status(id)                         -> {state, code, raw, prev_message}
wait_for_code(id, poll_interval)       -> code
claim_number(service, country_id, opts?) -> {id, phone}      // get_number + set_status(1)
get_code(id)                             -> code             // wait_for_code + set_status(6)
```

`get_status` normalizes the raw string into an enum — `WAIT_CODE`, `OK`, `CANCEL`,
`WAIT_RETRY`, `WAIT_RESEND`, `UNKNOWN` — plus the extracted code. **Always keep
the raw string on the returned object.** You will need it the day a vendor invents
a new response and your enum says `UNKNOWN`.

### Lifecycle — this is the part that costs money

```
1. get_number                  -> id, phone
2. set_status(id, 1)           <- IMMEDIATELY. marks the number ready.
                                  if this fails: set_status(id, 8) and bail.
3. poll get_status every ~3s until STATUS_OK:<code>, or ~2 min timeout
4. success -> set_status(id, 6)
   timeout/abort/error -> set_status(id, 8)
```

Skipping step 2 means paying for a number that will never receive anything.
Skipping step 4's cancel means paying for every failed attempt.

### Cleanup must outlive the cancellation

The single most common porting bug. Your claim/wait runs under a deadline or
cancellation token. The cleanup call that refunds your money **must not** inherit it:

| Language | Wrong | Right |
|----------|-------|-------|
| Go       | `SetStatus(ctx, …)` | `SetStatus(context.Background(), …)` |
| TS       | passing the same `AbortSignal` | fresh request, no signal (or a new one) |
| Python   | inside the cancelled task | `asyncio.shield(...)` or a fresh task |
| C#       | passing the same `CancellationToken` | `CancellationToken.None` |

Otherwise the timeout fires, you call cancel, and the cancel is itself cancelled.
The number stays rented and you're billed for it.

Related: in languages with unhandled-rejection semantics, these are fire-and-forget
best-effort calls. Swallow their errors explicitly rather than letting them escape
and kill the process — but log them, because a silently failing cancel is a slow
money leak.

### Poll loop details worth copying

- Fixed interval (~3s), not exponential backoff. The vendor's SMS arrives when it
  arrives; backing off just delays your detection of it.
- **Last-chance check on timeout.** Before giving up, do one final `get_status` on
  a *fresh* 10s deadline. Codes routinely land in the final second. This alone
  recovers a meaningful fraction of activations — it is the highest-value 10 lines
  in the whole package.
- **Transient network errors mid-poll: continue, don't abort.** A dropped
  connection on poll #14 is not a failed activation. Only a *parsed*
  `STATUS_CANCEL` / `NO_ACTIVATION` / `BAD_KEY` ends the loop early.
- If your runtime has no cancellable sleep, compute a deadline and check it each
  iteration rather than counting loops.

## 5. The facade

Provider is fixed at construction — session-like, not chosen per call.

```
config:
  provider          "herosms" | "smspool" | "smsbower" | "smsonline"
  api keys          one field per provider (all four, independent of choice)
  http_client       shared across providers, ~30s request timeout
  code_timeout      default 2 min
  poll_interval     default 3 s
  provider_extras   e.g. smspool pool / max_price — nullable, null = unset

methods:
  claim_number(country_iso) -> (activation_id: string, phone: string)
  get_code(activation_id)   -> code: string        // blocks up to code_timeout
  cancel_activation(activation_id)
```

Why this shape:

- **Construct all four sub-clients eagerly**, including unused ones. They hold a
  key and an HTTP client — cheap, and it keeps construction branch-free. Share one
  HTTP client/connection pool across all of them.
- **`claim_number` and `get_code` are separate calls**, deliberately. Not one
  blocking `get_number_and_code`. This lets you reserve a number early, paste it
  into a signup form, do other work, then block on the code later. Persist the
  `(provider, activation_id, phone)` triple if your flow can be resumed or crash
  mid-way — without it you cannot cancel an orphaned number and it bills out.
- **Normalize the provider string** (`sms_bower`, `sms-bower`, `sms bower` →
  `smsbower`) so config files and env vars don't have to be exact.
- **Provider-specific knobs as explicit nullable fields**, not an untyped options
  bag. Ugly, but greppable and checkable.
- Errors carry `{provider, operation, message, cause}` and stringify as
  `"smspool claim_number: NO_BALANCE"`. Worth the boilerplate the moment more than
  one vendor is in play — a bare `NO_BALANCE` tells you nothing about which
  account to top up. Preserve the underlying cause for unwrapping/chaining.

## 6. Country handling

Callers pass **ISO alpha-2** (`"US"`, `"PL"`). The facade translates to each
vendor's numeric country ID through a hardcoded lookup table.

- hero / bower / online **share one table** (~210 entries) — same upstream
  codebase, same IDs. Two of the three resolvers just delegate to the first and
  re-label the error.
- SMSPool numbers countries completely differently (`US: 1`, `GB: 2`, …) — its own
  table, no overlap in meaning. Do not reuse the other one.
- Unknown ISO → fail **before** any HTTP call.
- Virtual-number pseudo-countries (`US_V`, `AU_V`) are excluded. ISO only.

**Hardcode the tables.** The reference implementation's resolver takes an API key
and an HTTP client because it was built to fetch them live; it never does, and
that was the right call. The lists change maybe twice a year, and a live fetch adds
a failure mode on your critical path. Regenerate by hand from each vendor's
`getCountries` endpoint when needed.

In a language with a build step, generate the tables as a data file (JSON/TOML)
loaded once at startup rather than a giant source literal.

## 7. Usage

```
client = sms_manager({
    provider: config.sms_service,
    hero_key: ..., smspool_key: ..., smsbower_key: ..., smsonline_key: ...,
})

(id, phone) = client.claim_number("US")
phone = "+" + phone                     // vendors omit the leading +
... submit phone to whatever you're signing up for ...
code = client.get_code(id)              // blocks; auto-finishes on success

// on ANY abort path, with cleanup-safe cancellation:
client.cancel_activation(id)
```

## 8. Porting checklist

Build the minimum first: **one generic SMS-Activate client** parameterized by base
URL + service code, plus the two country tables. The four separate packages in the
reference implementation exist only because vendors drifted (bower's `ref`,
smspool's `setting=smspool` and string IDs). Start generic; fork a provider the day
it actually diverges, not before.

Non-negotiable, in rough order of how much they cost when missed:

- [ ] `set_status(id, 1)` immediately after claiming
- [ ] cleanup calls escape the cancelled context / token / signal
- [ ] cancel (`status=8`) on **every** abort path, including unexpected exceptions
- [ ] last-chance `get_status` before declaring timeout
- [ ] activation ID is a string end to end, including in storage
- [ ] parse the body, never the HTTP status
- [ ] transient poll errors continue; only parsed terminal states stop the loop
- [ ] `(provider, activation_id, phone)` persisted if the flow can crash mid-way
- [ ] raw response string retained on every parsed result, for the day the enum says `UNKNOWN`

Everything else is convenience.

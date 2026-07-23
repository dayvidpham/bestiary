# w3id.org registration runbook

A procedure the **user** executes by hand to register `https://w3id.org/bestiary/`
as a permanent-identifier namespace for bestiary entity IRIs. This document is a
runbook only: **no agent performs registration**, and none of the steps below have
been carried out. It exists so the decision (what the namespace resolves to, and
why) is recorded before the user files the PR, and so the URL scheme it commits to
is the same one already shipping in `iri.go`.

See [`CONCEPTS.md`](CONCEPTS.md) for the Entity/Nomen vocabulary and
[`research/v0.2.8-registry-ingest-creator.md`](research/v0.2.8-registry-ingest-creator.md)
(Topic Area 3) for the underlying research this runbook distills.

## 1. What w3id.org is

`w3id.org` (`perma-id/w3id.org`) is a community-run permanent-identifier service
operated under the W3C Permanent Identifier Community Group. It exists to give
projects a stable `https://w3id.org/<path>` namespace that outlives any one
project's hosting — the actual content lives wherever the project runs it, and
`w3id.org` only holds a redirect rule.

## 2. Registration procedure (GitHub PR + Apache `.htaccess`)

1. Fork `perma-id/w3id.org`.
2. Create a directory for the namespace: `bestiary/` at the repo root (the
   convention is one directory per registered path).
3. Add two files inside it:
   - **`.htaccess`** — the Apache `mod_rewrite` rules that redirect
     `w3id.org/bestiary/...` requests to bestiary's real host (§3 below has the
     concrete rules).
   - **`README.md`** — human-readable description + a maintainer contact (an
     email or a GitHub handle the w3id maintainers can reach if the target host
     ever moves).
4. Open a Pull Request against `perma-id/w3id.org`. A maintainer reviews and
   merges it (alternatively, mail `public-perma-id@w3.org` with the source path,
   the target URL, and the intended HTTP redirect code — the mailing-list path
   exists for cases that don't fit a PR).
5. Once merged, `https://w3id.org/bestiary/...` resolves per the `.htaccess`
   rules, immediately and with no further action.

**Norms to honor** (these are w3id's stated expectations of every registrant, not
bestiary-specific rules): the identifier must be treated as permanent — never
reused for something else once minted — and the registrant supplies a real
maintainer contact so the entry can be fixed or retired if the target host
changes.

**What this runbook does NOT do:** it does not fork the repo, does not write the
`.htaccess`, does not open the PR. Those are the user's actions; §3 below is the
`.htaccess` content the user would submit, written out so the design is concrete
enough to review before it's filed.

## 3. Redirect-target design: content negotiation, not a single target

An entity IRI (`https://w3id.org/bestiary/entity/<key>`) should be a **cool
URI** in the linked-data sense: it names the *model identity*, not any one
representation of it, and it redirects — via `Accept`-header content
negotiation — to whichever representation the requester can use:

- a browser (`Accept: text/html`) gets an HTML entity-detail page (this is the
  `cmd/bestiary-web` entity route, §17.6 of the v0.2.8 front-end proposal);
- a machine client (`Accept: application/ld+json` or `application/json`) gets a
  JSON-LD (or plain JSON) representation of the same entity.

w3id supports this natively — the negotiation happens in the `.htaccess` itself,
with no server-side logic beyond what already runs on the target host:

```apache
# w3id.org/bestiary/.htaccess — illustrative, not yet filed
RewriteEngine On
RewriteCond %{HTTP_ACCEPT} application/ld\+json [OR]
RewriteCond %{HTTP_ACCEPT} application/json
RewriteRule ^entity/(.*)$ https://<bestiary-web-host>/entity/$1 [R=302,L,QSA]
RewriteCond %{HTTP_ACCEPT} text/html
RewriteRule ^entity/(.*)$ https://<bestiary-web-host>/entity/$1 [R=302,L,QSA]
# default (no recognized Accept header): machine representation
RewriteRule ^entity/(.*)$ https://<bestiary-web-host>/entity/$1 [R=302,L,QSA]
```

Note the redirect target is **one route on both branches** —
`GET /entity/{ref}` on `cmd/bestiary-web` — because that route already carries
its own `Accept`-based content-negotiation seam (§17.6: "each entity handler
carries a content-negotiation seam now ... so the future w3id `Accept`-negotiated
redirect is a thin proxy in front of an already-negotiating canonical route, not
a later rewrite"). w3id's rewrite rules pick *which URL* to redirect to only if
bestiary ever splits HTML and JSON-LD onto separate paths (e.g. `.html` /
`.jsonld` suffixes); as designed today, one path negotiates internally and the
`.htaccess` can point everything at it unconditionally. Both shapes are valid
w3id patterns — this runbook records the simpler one as the default, and the
split-path form as the fallback if content negotiation ever needs to live at
the w3id layer instead of the app layer.

**Blocker, stated plainly:** none of this can be filed until `cmd/bestiary-web`
(§17 of the v0.2.8 front-end proposal) is deployed at a stable public host.
Registering the namespace before a host exists would point
`w3id.org/bestiary/` at nothing.
This is why R6 explicitly scopes this deliverable as documentation only, with
registration deferred to the user's own judgment about when a host is ready.

## 4. The default IRI base

The IRI base is **`https://w3id.org/bestiary/entity/`**, supplied at the
`EntityRef.IRI(base)` **call site** — never hardcoded into `iri.go`. This
follows directly from how `IRI(base)` is already designed (see `iri.go`'s
doc comment on `mintIRI`): bestiary does not own a public namespace and
`IRI` takes the base as a parameter precisely so it never has to. Registering
`w3id.org/bestiary/` does not change `iri.go` at all — it only gives callers
(the future `cmd/bestiary-web`, any RDF/JSON-LD export, external consumers) a
real value to pass where today they'd pass a `localhost` or internal base:

```go
ref.IRI("https://w3id.org/bestiary/entity/")
// → "https://w3id.org/bestiary/entity/llama%2Fscout%404%2317b-16e%7Binstruct%7D"
```

The `entity/` segment is deliberate and leaves room in the `bestiary/`
namespace for future non-entity paths (a series/release IRI, a vocabulary
document, etc.) without a migration.

## 5. URL scheme (RQ1 ruling)

The entity path segment is the **percent-encoded canonical key**, exactly as
`EntityRef.IRI` already renders it — this is a ratification amendment (RQ1)
on the v0.2.8 proposal and is restated here because it is the one fact this
runbook and the `cmd/bestiary-web` URL scheme (§17.4) must agree on
byte-for-byte:

- The canonical key's own multi-segment structure (`family/variant@version#size{mods}`)
  is carried through **literally** — `/` inside the key is NOT re-escaped into
  a single opaque token. A key with a variant renders as a genuine multi-segment
  path (`.../llama%2Fscout%404...` is one escaped segment per `escapeIRISegment`'s
  contract — see `iri.go`: the whole canonical string is escaped as ONE path
  segment, so `/` *inside* the key is percent-encoded to `%2F`, never left raw).
  The ruling's point is about which characters get escaped, not about
  introducing new literal path segments beyond the one the key already is.
- `@`, `#`, `{`, `}` are percent-encoded (`escapeIRISegment` in `iri.go` handles
  this: `url.PathEscape` for `#{}` and an explicit `@` → `%40` pass, since `@`
  is a legal `pchar` that `PathEscape` would otherwise leave raw).
- This is a **documented v0.2.7 → v0.6.0 IRI output change** — `escapeIRISegment`
  was aligned to this ruling, so IRIs minted before and after the ruling differ
  byte-for-byte for any key containing `@`, `#`, `{`, or `}`. Any external
  consumer that captured a pre-ruling IRI must re-mint it.
- **Query parameters are view-state only, never identity.** A `?family=llama`
  or `?sort=price` on a `cmd/bestiary-web` list URL is UI state (filters,
  sort order) that Datastar's `ReplaceURLQuerystring` writes for shareable
  filtered views (§17.4/§17.6) — it is never part of what an entity IRI
  *names*. The w3id redirect rules operate on the path only; a query string
  passed through a w3id redirect (`[QSA]` in §3's rules) rides along as
  view-state on the target, not as part of the entity's identity.

## 6. Summary of what the user still has to do

1. Stand up `cmd/bestiary-web` at a stable public host (§17, separate slices).
2. Fork `perma-id/w3id.org`, add `bestiary/.htaccess` + `bestiary/README.md`
   following §3, pointing at that host.
3. Open the PR; once merged, pass `https://w3id.org/bestiary/entity/` as the
   base at every `EntityRef.IRI(base)` call site that should mint a public,
   resolvable IRI.

No code in this repository changes as a result of registering the namespace —
`IRI(base)` already accepts whatever base the caller supplies.

# w3id.org registration runbook

A procedure the **user** executes by hand to register `https://w3id.org/bestiary/`
as a permanent-identifier namespace for bestiary entity IRIs. This document is a
runbook only: **no agent performs registration**, and none of the steps below have
been carried out. It exists so the decision (what the namespace resolves to, and
why) is recorded before the user files the PR, and so the URL scheme it commits
to is pinned down precisely — **including where that scheme is still ahead of
what `iri.go` ships today** (§5 states the gap and the precondition it implies
for registration).

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

## 5. URL scheme (RQ1 ruling) — ratified target vs. what ships today

RQ1 (the ratification amendment on the v0.2.8 proposal) rules on the URL
scheme the `w3id.org/bestiary/entity/` namespace and the `cmd/bestiary-web`
URL scheme (§17.4) must agree on byte-for-byte. This section states the
ruling honestly in three parts, because as of **this commit the shipped
code does not yet implement it** — conflating "ratified" with "shipped"
here would make a compliance claim this runbook cannot back.

**(a) Ratified target (RQ1, verbatim intent).** Entity URLs are
**multi-segment**: the canonical key's own structure
(`family/variant@version#size{mods}`) is carried through as **literal `/`
path separators**, so a key with a variant is a genuine multi-segment path
(e.g. `.../llama/scout@4#17b-16e%7Binstruct%7D`, not one opaque
percent-escaped blob). Within that multi-segment path, `@`, `#`, `{`, `}`
are percent-encoded (they are identity-grammar delimiters, not path
separators). Query parameters are **view-state only, never identity** — see
the query-parameter bullet below, which already reflects the ratified
behavior. RQ1 is recorded as a **v0.2.7 → v0.6.0 IRI output change**: once
implemented, IRIs differ byte-for-byte from every IRI minted before it, for
any key containing `/`.

**(b) Current shipped behavior (this commit).** `EntityRef.IRI` /
`escapeIRISegment` (`iri.go`) does **not** implement (a) yet: the *whole*
canonical string — including every `/` inside it — is escaped as **one**
opaque path segment via `url.PathEscape` (`#`/`{`/`}` percent-encoded by
`PathEscape` itself; `@` percent-encoded by an explicit follow-up pass,
since `PathEscape` alone leaves `@` raw). Today's actual output for a
variant-bearing key is:

```go
ref.IRI("https://w3id.org/bestiary/entity/")
// → "https://w3id.org/bestiary/entity/llama%2Fscout%404%2317b-16e%7Binstruct%7D"
//                                       ^^^^^ literal '/' is %2F-escaped here — NOT (a)'s literal-'/' target
```

This is the same example shown in §4 above — it is today's real,
correct-for-today output, not a mistake, but it is **pre-RQ1**: `/` inside
the key is still an opaque escape, not a path separator.

**(c) Sequencing — a registration precondition, not a registration blocker.**
Aligning `escapeIRISegment` to leave `/` literal is scheduled work within
this same v0.2.8 release (owned by the web-core slice, alongside
`cmd/bestiary-web`'s own URL scheme, §17.4 — the two must land together
since they share the one grammar). **The user MUST NOT file the w3id
registration PR (§2) against a build where (b) still holds.** Concretely,
add this as an explicit precondition step ahead of §2 step 1:

> **Precondition:** confirm `iri.go`'s `escapeIRISegment` has been aligned
> to RQ1 (literal `/` path separators; `@`/`#`/`{`/`}` still percent-encoded)
> before registering the namespace or publishing any `w3id.org/bestiary/entity/…`
> link. Registering (or publicizing) an IRI shape that a later code change
> will silently alter is exactly the kind of identifier instability w3id's
> own permanence norm (§2) exists to prevent.

**Query parameters are view-state only, never identity** (this part of the
ruling holds under both (a) and (b) — it is unaffected by the `/`-escaping
question). A `?family=llama` or `?sort=price` on a `cmd/bestiary-web` list
URL is UI state (filters, sort order) that Datastar's
`ReplaceURLQuerystring` writes for shareable filtered views (§17.4/§17.6) —
it is never part of what an entity IRI *names*. The w3id redirect rules
operate on the path only; a query string passed through a w3id redirect
(`[QSA]` in §3's rules) rides along as view-state on the target, not as
part of the entity's identity.

## 6. Summary of what the user still has to do

1. **Precondition (§5c): confirm `escapeIRISegment` has landed RQ1's literal-`/`
   alignment** in the build being deployed — check `iri.go` against §5(a)/(b)
   above, or check whether the web-core slice's `cmd/bestiary-web` landing has
   already brought it in. Do not proceed past this step on a pre-alignment
   build.
2. Stand up `cmd/bestiary-web` at a stable public host (§17, separate slices).
3. Fork `perma-id/w3id.org`, add `bestiary/.htaccess` + `bestiary/README.md`
   following §3, pointing at that host.
4. Open the PR; once merged, pass `https://w3id.org/bestiary/entity/` as the
   base at every `EntityRef.IRI(base)` call site that should mint a public,
   resolvable IRI.

No code in this repository changes as a result of registering the namespace —
`IRI(base)` already accepts whatever base the caller supplies. Step 1 is the
only precondition on *code* state; steps 2-4 are the user's own actions.

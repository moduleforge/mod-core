# Project Roadmap

## Purpose and scope

Long-term planning for `mod-core`: possible features and architectural changes beyond the active
implementation plan in `plan/`. Items here are open design questions or deferred ideas, recorded so
a future engineer understands them as direction under consideration, not forgotten scope and not
part of the current code.

## Roadmap overview

No version-committed work is recorded here yet. Three items are recorded, all open:

- [Observer registration seal, mediated late-stage Add, and app-scoped Remove](#observer-registration-seal-mediated-late-stage-add-and-app-scoped-remove)
  tightens the observer registration surface. It is open and undecided.
- [Typed authorization target](#typed-authorization-target) makes the authorization target typed.
  It is open and undecided.
- [Isolation of managed application functionality](#isolation-of-managed-application-functionality)
  records the investigation and decisions for an open-source release with only standalone
  deployment public. The direction is decided; implementation is deferred.

## Possible future goals

### Observer registration seal, mediated late-stage Add, and app-scoped Remove

**Not implemented now.** This is an open design question. Origin: followup 5cqV (security, low
priority).

Today `ObserverGroup.Add` is callable by any holder of the group pointer at any time, and there is
no `Remove`; see [Observer Registration](./architecture/observer-registration.md) for the current
behavior and trust model. Options to consider:

- **(a) Seal after startup.** A `Seal`/`Freeze` the host calls once startup completes, after which
  `Add` errors or panics. Hosts that need late registration would opt in explicitly, for example
  via an option at group construction. Alternatively, keep `Add` open but require a mandatory audit
  log entry for every post-startup `Add`.
- **(b) Mediate late-stage `Add`.** Access to the group should not be a bare pointer any module can
  use. Reachability depends on how the mfgen-generated root exposes the group to modules, so this
  may need an mfgen change.
- **(c) App-scoped `Remove`.** If dynamic adding is supported, also provide a `Remove`, where an
  app may remove only observers it added itself (for example, `Add` returns a handle or token that
  `Remove` requires, or ownership is tracked per registrant). Never another registrant's observer,
  and never a module-provided one.

### Typed authorization target

**Not implemented now.** This is an open design question. Origin: followup ogPn.

`Authorizer.Authorize` takes an untyped `*int64` target, which the contract defines as an
`entities.id`. A `types.id` is also an `int64`, so the compiler cannot stop a caller from passing
one, and an entity-semantic implementation then matches it against an unrelated entity. The
optional `authz.TypeAuthorizer` capability and `authz.AuthorizeType` helper (see
[Type-Level Authorization](./architecture/type-level-authorization.md)) separate type-level checks
from entity-level ones without changing the `Authorize` signature. A longer-term option is a typed
`Target` value that distinguishes entity targets from type targets at compile time. It would break
every `Authorizer` implementation, decorator, and stub across the ecosystem, so it needs a
coordinated migration and is not planned.

### Isolation of managed application functionality

> **Private-side note: do not publish this section.** It describes private-side intent, including
> the existence and shape of the secret managed contract. This section must be excluded or removed
> from `mod-core`'s copy of this file before any public snapshot, because `docs/project-roadmap.md`
> may otherwise ship publicly.

**Not implemented now.** The direction below is decided, but the work is parked and code will move
before it starts. Treat the decisions and rationale as durable and re-verify every code-level claim
before acting on it.

#### Goal and motivation

Release ModuleForge (the `mod-*` modules, the `app-*` applications, the `mfgen` compiler, and
`docs-mf-standards`) as open source so that people can use the open-source modules to build their
own standalone apps. The managed app ecosystem is offered separately, as a personal-cloud
alternative to centralized commercial services. The public releases support **standalone**
deployment only, locally and in the cloud. **Managed mode** (apps deployed into and administered by
`app-mfmanager`) stays private. Keeping the managed-mode logic private is critical.

#### Decisions made

- **License.** Apache-2.0 for every public repo, with DCO sign-off on contributions. No CLA.
- **Standalone tenancy.** One database owner/user accesses all tables directly. The app still
  supports multiple users and multiple organizations within the common database, each limited to
  its own data. That isolation is application-level, enforced by the authz grant model, not
  database-level.
- **Scope.** `mod-billing` and `mod-tokens` are in scope for public standalone apps.
- **Managed scope.** Only `app-mfmanager` is private by definition. Other apps, and therefore the
  modules they compose, remain deployable in managed mode.
- **Secrecy.** The app-side managed contract is secret, not only the control plane.
- **Delivery of private logic.** A private overlay layer on top of the public repos, not a fork per
  repo.
- **Publishing.** Fresh-snapshot public repos with no history rewrite. Full history stays in the
  archived private repos. Public `git blame` starts at the snapshot.
- **Selection.** Build-time layer selection replaces the fail-open runtime selector.

#### Findings

- **`app-mfmanager`** is the control plane and is entirely private. It needs no splitting.
- **`app-mftodo`** is the only managed app today. Its managed glue is a small set of managed-only
  files plus run-mode branches in mixed `appsetup` files, managed `infra:` entries, most startup
  hooks in its app manifest, and a GUI flag that hides local login.
- **Modules and `mfgen`** carry only small, mostly generic seams: the `mod-core` write-function
  wrappers, `mod-users`' externally-managed sign-in marker, the platform-injected database URL
  alias, `mod-repos`' public-visibility ceiling, and the `mfgen` skip-migrations knob.
- **The main leakage risk is prose and history**: comments, docs, plan records, registry files,
  test fixtures, commit messages, and the immutable `mod-core` migration comment that cites the
  private design. Most repos need a scrub and a fresh public start, not a private downstream.
- **Today's managed and standalone app are the same image** selected by an environment variable.
  Removing managed code from the public tree means the managed catalog must point at a separately
  built managed image.
- **Fail-open selector.** An absent run-mode variable silently boots a managed deployment as
  standalone. Build-time selection removes this.

#### Options considered

| Option | Summary | Verdict |
|---|---|---|
| A | Public upstream, private downstream fork per repo | Rejected. Permanent merge tax, implicit boundary, leakage by cherry-pick, doubled repos and CI |
| B | Manifest overlays composed by `mfgen`, built into a separate private managed image | Adopted. Matches the existing app-logic entry points (`startupHooks:`, `infra:`); purely additive; no merge tax |
| C | Runtime ports and adapters in public, managed adapters in private | Narrow use only. Broad use is speculative abstraction and describes the private product in public signatures; Go `plugin` is unsuitable |
| D | Single private source filtered into public mirrors | Rejected. Highest leakage risk, brittle stripping, weak contributor experience |
| E | Neutral, generic public capabilities with a private control plane | Adopted for generic seams. Alone it would make the app-side contract public, which is decided against |

**Recommendation: B + E, with C only where needed. Reject A and D.** Modules, `mfgen`, and the
standards are neutralized (E) and published with no private downstream. Managed apps get a public
standalone repo plus a private overlay repo (B) that builds the managed image. Typed ports (C) are
added only where a public call site must stay but its behavior differs, for example the auth
surface the GUI reads. The overlay feature is worth building even if the contract were later made
public, because it is a general extension mechanism.

#### Design direction and extension points

These are proposals for the manifest spec, not valid format today.

- **`extends:`** lets a private app manifest layer on a public one. Single-level at first. Map
  sections deep-merge, and redefining a key requires an explicit `override: true`.
- **Hook ids and ops.** Startup hooks gain stable ids plus `position: first|last`. An overlay uses
  `insert`, `replace`, and `remove` ops that address base ids.
- **`handlerWrappers`** is a first-class handler-wrapping point, replacing wrapping `srv.Handler`
  from a startup hook.
- **`migrations.generated: false`** is the build-time equivalent of the skip-migrations variable.
- **Infra `phase: beforeServices`** replaces the dummy-consumer trick used to survive reachability
  pruning.
- **Validation rules V10-V13.** V9 is already taken by the implemented Go module identity rule, so
  the new overlay rules are numbered V10 through V13.
- **Module `platform:` field.** A declarative, neutral manifest field (entity write types, access
  function slugs, write-function seams) replacing cross-repo `go/ast` parsing of module source.
- **Neutral GUI runtime-config auth contract.** A capability shape such as
  `auth.localSignIn` plus `auth.notice` replaces a managed-mode flag, so public GUIs need no
  private code.
- **Neutral capability names** for generic seams, for example an external-sign-in marker in place
  of the externally-managed one, each justified by a standalone use case.
- **Build-time layer selection** replaces the fail-open run-mode selector.
- **Public/private contribution flow.** Development happens in public. Private overlays consume
  public changes by a pin bump. A nightly private job builds every overlay against public HEAD. A
  managed-motivated public change must stand on standalone merit.

**Known spec gap.** An overlay inherits the base's startup hooks, but hook packages such as
`appsetup` live inside the base's generated Go module. The overlay's generated `go.mod` has
`replace` directives only for `modules[]` entries, so an inherited import into the base's module
cannot resolve. The natural design is for `mfgen` to add a `require` and `replace` for the base's
Go module to the overlay's `go.mod` when the effective imports reference it, which requires the
base to be generated first. This is not yet decided in the spec.

#### Indicative phased path

The architect estimated **9-15 engineer-weeks** end to end, dominated by prose scrubbing and
testing. This is a rough single-engineer estimate, not a commitment.

0. **Decisions and measurement.** Settle open questions. Write a repeatable deny-list scan over
   tracked files that becomes the publish gate. Target zero managed-design or private-name hits per
   public repo.
1. **Spike on `app-mftodo`.** Prototype the overlay mechanism in `mfgen` and split `appsetup` into
   a standalone package plus a managed package. Generate both binaries and run both suites.
2. **Standards and `mfgen` changes.** Spec, compiler, and conformance fixtures, including a public
   overlay fixture so public CI exercises the feature without private code.
3. **Scrub and publish modules in dependency order.** Standards and `mfgen`, `mod-core`, the
   core-only modules, `mod-users`, `mod-repos` and `mod-billing`, then `mod-tokens` (which depends
   on billing in both Go and schema).
4. **Split and publish apps.** `app-mftodo` becomes a public standalone app plus a private overlay
   that builds the managed image. `app-mfmanager`'s catalog image reference, lockfile overrides,
   and drift tests are repointed.
5. **Steady state.** Deny-list gate in public CI, the nightly overlay canary, and the standalone
   merit review rule.

The federated plan (`oss-standalone-split`, in `docs-mf-standards` and `mfgen`) breaks the
standards and compiler side into eight standards phases and eight `mfgen` phases. In outline:

- Standards: foundations (docs tooling, decision records, publish gate and scan, spec baseline
  reconciliation), spec draft (overlays and hooks, `platform:`, runtime-config auth contract), spec
  finalization with conformance examples, architecture docs, OSS governance (community files, CI,
  topology, lockfile overrides, consumers registry split), publication procedures, publish
  readiness, and a final doc review.
- `mfgen`: baseline and test harness, extension schema and conformance data, hook ops and codegen,
  overlay loading and merge, conformance runner and layer-selection CLI, OSS readiness, publish
  readiness verification, and doc updates.

Follow-on plans, each in its own repo and run afterwards, cover the `app-mftodo` spike, per-repo
scrub and publish, the `app-mftodo` overlay split, lockfile overrides support in the pin scripts,
and a private companion repo for the private deny-list, registry, and relocated docs.

#### Apache-2.0 and DCO setup

Not yet applied to any repo. This can be done independently and early, because it does not depend
on the code moving, and it must be in place in each repo before that repo's public snapshot. Each
repo gets:

- `LICENSE`: Apache-2.0.
- `NOTICE`: the copyright holder, currently TBD (see the open questions).
- `CONTRIBUTING.md`: the DCO sign-off policy (`git commit -s`, producing a `Signed-off-by` line),
  plus the rule that managed-motivated changes must stand on standalone merit.
- A DCO-check CI workflow implemented in the repo itself, with no third-party app (the current
  default).
- `SECURITY.md` and `CODE_OF_CONDUCT.md`.

#### Risks

- **Managed-design prose ships publicly.** Likely without a gate and irreversible once public.
  Mitigate with the deny-list gate in pre-publish and CI, the fresh snapshot, and two-person review
  of the first publish.
- **Overlay semantics silently shadow base config.** Mitigate with required `override: true`,
  id-based ops, the V10-V13 rules, and overlay fixtures in `mfgen` tests.
- **Public refactors break overlay hook ids.** Treat hook ids, exported hook-package signatures, and
  manifest fields as semver API, and run the nightly canary.
- **`mod-tokens` depends on `mod-billing`** in Go and in schema. Both are in scope, but publish
  order must respect it.
- **Shared-database grant model misread as multi-tenant-safe.** Public docs must state the
  single-database-owner trust assumption and the application-level nature of tenant isolation.
- **Renamed or recreated repos lose GitHub rename redirects.** Inventory remote URLs in lockfiles,
  registries, and CI before transfer.

#### Open owner questions

- Copyright holder legal name and year range (individual or entity), for `NOTICE`.
- Whether `style-liquid-labs`, `app-dms`, `app-lexsocius`, and `app-mfpost` go public.
- Security-report channel: GitHub private vulnerability reporting (default) or an email address.
- Code of conduct choice (default: Contributor Covenant 2.1) and its enforcement contact.
- Whether spec finalization waits for the `app-mftodo` spike. The default is that it does not, and
  spike findings arrive later as spec amendments.
- DCO check mechanism: in-repo workflow script (default) or a third-party app.

#### Coordination notes

- Hold `app-mfgit`'s pending managed-mode work until the overlay mechanism exists, so it is built
  directly as an overlay rather than in-tree and then extracted.
- A concurrent standards plan, `mf-standards-update-and-sync`, adds a doc describing the private
  mode. That doc must not be public.
- Followup `0dPP` asks a public repo to document private mode. That conflicts with this direction
  and should be redirected to a private repo.
- Related findings: lockfile `overrides` are not implemented in the `app-mftodo`, `app-mfgit`, and
  `mod-users` scripts; the V6 and V7 spec rules are not implemented in `mfgen`; and generated
  `main.go` and `go.mod` embed absolute local paths.

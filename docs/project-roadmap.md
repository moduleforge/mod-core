# Project Roadmap

## Purpose and scope

Long-term planning for `mod-core`: possible features and architectural changes beyond the active
implementation plan in `plan/`. Items here are open design questions or deferred ideas, recorded so
a future engineer understands them as direction under consideration, not forgotten scope and not
part of the current code.

## Roadmap overview

No version-committed work is recorded here yet. The one item below tightens the observer
registration surface; it is open and undecided.

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

  ▎ Review the changes on this feature branch for upstream contribution readiness. This PR adds opt-in availability monitoring — health
   checks, connection timing, error classification, and a dashboard template.

  ▎ Check these specific areas:

  ▎ 1. Are we overstepping?
  ▎ - Did we modify the root Makefile, CI workflows, or build scripts beyond what our feature requires? Adding -race to existing test
  targets is defensible; adding new targets, changing output format, or adding coverage tooling is opinionated.
  ▎ - Did we change files like README.md, docker-compose.yml, or Dockerfiles for convenience (ARM compat, formatting) rather than
  functional necessity? Those should be separate PRs.
  ▎ - Are we renaming variables, reformatting imports, or touching code we didn't need to change?

  ▎ 2. Is it truly opt-in?
  ▎ - Run the integration with zero new flags set. Does it produce identical output to the upstream master branch? No new event types,
  no new attributes, no changed behavior.
  ▎ - Are all new argument defaults safe (false, "", 0)?
  ▎ - Does openSQLDB (or equivalent legacy path) still work unchanged?

  ▎ 3. Security audit
  ▎ - git log -p the entire branch — any real credentials, account IDs, API keys, license keys in any commit (including ones that were
  later "fixed")?
  ▎ - Do all error messages pass through credential sanitization before reaching external systems?
  ▎ - Are test passwords clearly scoped to Docker test environments?

  ▎ 4. Test integrity
  ▎ - Do any tests reference flags, functions, or APIs that don't exist? (Silent false positives — tests that pass because they test
  nothing)
  ▎ - Are there slice aliasing bugs in loops that append to a shared base slice?
  ▎ - Is shared state (accumulators, timing structs) properly synchronized, or safe due to single-threaded execution pattern?

  ▎ 5. Dashboard template (if included)
  ▎ - Any real account IDs in the template JSON?
  ▎ - Do variable queries use attributes guaranteed to exist (hostname, entityName) vs ones that require label configuration
  (label.instance)?
  ▎ - Do billboard thresholds match the actual value range of the NRQL function (e.g. percentage() returns 0-1, not 0-100)?

  ▎ The key question: are we being a good guest? Every change should serve the feature. If removing a change wouldn't break the
  feature, it probably shouldn't be in this PR.

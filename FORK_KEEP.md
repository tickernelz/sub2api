# Fork keep set

`AGENTS.md` is the canonical keep/replay document. This file is a pointer only.

Fork-only behavior that must survive upstream synchronization is recorded in
`AGENTS.md` section 2, "Fork keep-set". Read that section before replaying or
resolving any fork commit, and update it there rather than here.

The provider-aware gateway `service_tier` feature that this file used to
specify in full now lives in `AGENTS.md` section 2.D, including its supported
account routes, replay anchors, and known hazards.

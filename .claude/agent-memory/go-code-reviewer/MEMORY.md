# donezo (github.com/bgrewell/donezo) — reviewer memory index

- [Architecture: space-per-file SQLite](architecture_donezo_store.md) — core.db registry + one SQLite file per space; ownedSpace() pattern
- [classifyConstraint convention](pattern_classify_constraint.md) — sentinel-error mapping from SQLite codes; where it's easy to miss a spot
- [dev-auto-login requires --seed](gotcha_dev_auto_login.md) — StaticAuthenticator hardcodes user id=1; breaks FK-backed writes if unseeded
- [Test style conventions](testing_conventions_donezo.md) — table-driven + t.Parallel() + tt:=tt capture, newTestServer fixture shape
- [Connection-cache design tradeoff](note_connection_cache_growth.md) — SpaceStore.conns never evicts; known/accepted for personal-scale use
- [Roles + invites security review](feature_roles_invites.md) — atomic-claim pattern to reuse; compensation-atomicity gap; invite code case-sensitivity bug

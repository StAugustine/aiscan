# Archived Reflex tests

These files preserve the earlier Reflex tests before the API 2 native-contract
implementation. The `.go.txt` suffix keeps historical assertions out of Go's
active test build. Current coverage lives in `v2_*_test.go`,
`native_mechanism_test.go`, `compiler_repair_test.go` and the integration tests
under `exts/jev`.

Files prefixed with `master-` preserve additional version 1 tests from the
current mainline when version 2 was integrated.

Business-specific verification callbacks are test oracles only. Production
qualification uses native contracts, exact recorded replay and independent JEV
review; it does not load this archive or require a business verification suite.

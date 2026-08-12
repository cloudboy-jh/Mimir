# Release Evidence

Mimir releases are promoted from immutable candidate artifacts. Tag pushes do
not rebuild binaries and fail unless matching approved evidence exists.

1. Dispatch `release-candidate` with a prospective semver tag and the full
   commit SHA from `master`.
2. Download `release-candidate-<tag>` and verify `candidate.json` against the
   payload with `bun run release:evidence -- validate` after creating the
   evidence manifest.
3. Install the candidate through the supported bootstrap path and capture
   redacted human and JSON transcripts for every check required by
   `scripts/release-evidence.ts`. Real Cloudflare checks remain local. Use
   `/whoami` and direct session APIs; never call a paid model endpoint.
4. Record only transcript SHA-256 hashes and sizes in `evidence.json`. Never put
   account IDs, URLs, tokens, email addresses, or transcript bodies in it.
5. Base64-encode `evidence.json` and dispatch `release-evidence` with the
   candidate workflow run ID. The protected `release` environment owns human
   approval.
6. Push the tag only after `release-evidence-<tag>` exists. The tag workflow
   verifies the candidate, evidence, tag, and commit before publishing the
   exact tested payload.

Required checks:

- `bootstrap-install-clean-home`
- `install-human` and `install-json`
- `update-human` and `update-json`
- `doctor-human` and `doctor-json`
- `deploy-real-cloudflare`
- `failed-deploy` and `failed-deploy-recovery`
- `existing-install-receipts`
- `custom-cloudflare-resource-names`
- `stale-cached-metadata`
- `owned-artifacts-doctor-clean`

Every check must be `pass`. Missing, duplicate, stale, mismatched, or modified
evidence blocks publication. Expired candidate or evidence artifacts require a
new candidate and another validation run.

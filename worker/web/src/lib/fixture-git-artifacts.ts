import type { GitArtifact } from "./api";
import { fixtureIso } from "./fixture-conversations";

export const hierarchySha = "7ad8d9e43a61c59fe22379f8e5ca68dbe8c41120";
export const motionSha = "2bc914739961f48fb38d6a77fda4be605b703c19";
export const cleanupSha = "8d38cfa970e649d5137ac20dff6c98368c1d70e4";
export const healthSha = "105e26074fb3112c48c0f99cf4f5bf900bf24b21";
export const sampleHierarchyPatch = `diff --git a/worker/web/src/components/session/SessionHeader.vue b/worker/web/src/components/session/SessionHeader.vue
index 1122334..2233445 100644
--- a/worker/web/src/components/session/SessionHeader.vue
+++ b/worker/web/src/components/session/SessionHeader.vue
@@ -10,3 +10,6 @@
-  <div class="model-stack">
+  <section aria-labelledby="result-evidence-heading">
+    <SessionChanges />
+  </section>
+  <div class="model-tree">
   </div>
   <SupportingSessions />
diff --git a/worker/web/src/components/session/SessionChanges.vue b/worker/web/src/components/session/SessionChanges.vue
new file mode 100644
index 0000000..3344556
--- /dev/null
+++ b/worker/web/src/components/session/SessionChanges.vue
@@ -0,0 +1,5 @@
+<template>
+  <section aria-labelledby="result-evidence-heading">
+    <a :href="patchUrl">Inspect captured patch</a>
+  </section>
+</template>
`;
export const sampleReviewPatch = `diff --git a/worker/web/src/components/session/SessionHeader.vue b/worker/web/src/components/session/SessionHeader.vue
index 1122334..2233445 100644
--- a/worker/web/src/components/session/SessionHeader.vue
+++ b/worker/web/src/components/session/SessionHeader.vue
@@ -10,3 +10,6 @@
-  <div class="model-stack">
+  <section aria-labelledby="result-evidence-heading">
+    <SessionChanges />
+  </section>
+  <div class="model-tree">
   </div>
   <SupportingSessions />
`;
const motionPatch = `diff --git a/worker/web/src/styles.css b/worker/web/src/styles.css
index 5566778..6677889 100644
--- a/worker/web/src/styles.css
+++ b/worker/web/src/styles.css
@@ -7,2 +7,5 @@
-  --animate-panel-in: panel-in 160ms ease-out;
+  --animate-panel-in: panel-in 200ms cubic-bezier(0.16, 1, 0.3, 1);
+}
+@media (prefers-reduced-motion: reduce) {
+  .overlay { animation: none; }
 }
`;
const cleanupPatch = `diff --git a/scripts/publish.sh b/scripts/release.sh
similarity index 100%
rename from scripts/publish.sh
rename to scripts/release.sh
diff --git a/scripts/check.sh b/scripts/check.sh
old mode 100644
new mode 100755
diff --git a/legacy.json b/legacy.json
deleted file mode 100644
index 1122334..0000000
--- a/legacy.json
+++ /dev/null
@@ -1,3 +0,0 @@
-{
-  "legacy": true
-}
diff --git a/assets/sample-icon.png b/assets/sample-icon.png
index aabbccd..bbccddee 100644
Binary files a/assets/sample-icon.png and b/assets/sample-icon.png differ
`;
const healthPatch = `diff --git a/src/health.ts b/src/health.ts
index 1122334..2233445 100644
--- a/src/health.ts
+++ b/src/health.ts
@@ -1,2 +1,2 @@
 const response = await health();
-process.exit(response.transport === "ok" ? 0 : 1);
+process.exit(response.ready ? 0 : 1);
`;
const receiptPatch = `diff --git a/src/capture-state.ts b/src/capture-state.ts
index 1122334..2233445 100644
--- a/src/capture-state.ts
+++ b/src/capture-state.ts
@@ -1,2 +1,2 @@
 export function state(receipt: Receipt) {
-  return receipt.accepted ? "saved" : "pending";
+  return receipt.savedAt ? "saved" : "pending";
`;
const rejectedSyncPatch = `diff --git a/src/session-sync.ts b/src/session-sync.ts
index 1122334..2233445 100644
--- a/src/session-sync.ts
+++ b/src/session-sync.ts
@@ -1,2 +1,2 @@
 export function canSync(session: Session, user: User) {
-  return session.ownerId === user.id;
+  return session.shared;
`;

type Seed = {
  sessionId: string; sha: string; parent?: string; ago: number; subject: string; url: string | null;
  ref: string; patch: string; status?: GitArtifact["capture_status"];
};
const seeds: Seed[] = [
  { sessionId: "ses_fixture_multi_model_result", sha: hierarchySha, ago: 44, subject: "Sample: restore result evidence hierarchy", url: "https://github.com/example/mimir", ref: "feature/dashboard-evidence", patch: sampleHierarchyPatch },
  { sessionId: "ses_fixture_multi_model_result", sha: motionSha, parent: hierarchySha, ago: 21, subject: "Sample: respect reduced motion in dashboard overlays", url: "https://github.com/example/mimir", ref: "feature/dashboard-evidence", patch: motionPatch },
  { sessionId: "ses_fixture_harness_claude_code", sha: hierarchySha, ago: 44, subject: "Sample: restore result evidence hierarchy", url: "https://github.com/example/mimir.git", ref: "review/evidence", patch: sampleReviewPatch },
  { sessionId: "ses_fixture_harness_oh_my_pi", sha: cleanupSha, ago: 10, subject: "Sample: clean release scripts and binary assets", url: "https://gitlab.com/example/sample-cli", ref: "chore/release", patch: cleanupPatch },
  { sessionId: "ses_fixture_harness_pi", sha: healthSha, ago: 8, subject: "Sample: signal unready services with exit status", url: "https://gitlab.com/example/sample-cli", ref: "fix/readiness", patch: healthPatch },
  { sessionId: "ses_fixture_active_capture", sha: "d0259f6904bc44e6a86b9f7c06a4f72946b4ad53", ago: 1, subject: "Sample: preserve pending receipt state", url: "https://github.com/example/mimir", ref: "fix/receipt", patch: receiptPatch, status: "accepted" },
  { sessionId: "ses_fixture_failed_work", sha: "f30e8b01161e4d40b6a8fe4c1792dfab1a2fe050", ago: 332, subject: "Sample: rejected ownership experiment", url: "https://github.com/example/mimir", ref: "experiment/session-sync", patch: rejectedSyncPatch, status: "failed" },
  { sessionId: "ses_fixture_unknown_repo", sha: healthSha, ago: 30, subject: "Sample: local repository without a configured remote", url: null, ref: "local/health", patch: healthPatch },
  { sessionId: "ses_fixture_unknown_repo_other", sha: healthSha, ago: 31, subject: "Sample: another local repository without a remote", url: null, ref: "local/health", patch: healthPatch },
  { sessionId: "ses_fixture_active_capture", sha: "46afe23c6c661e3477a07c9e9f6490cab8df9835", ago: 2, subject: "Sample: await worker receipt", url: "https://github.com/example/zz-capture-worker", ref: "main", patch: receiptPatch, status: "accepted" },
  { sessionId: "ses_fixture_failed_work", sha: "bd7739c9c293d26813119490e7c259d45f4f7cf6", ago: 333, subject: "Sample: failed synchronization capture", url: "https://gitlab.com/example/zz-sync-worker", ref: "experiment/sync", patch: rejectedSyncPatch, status: "failed" },
];

// Compute real digests for these fictional patches; consumers can compare variants truthfully.
export const fixtureArtifactRows = Promise.all(seeds.map(async (seed) => {
  const bytes = new TextEncoder().encode(seed.patch);
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  const status = seed.status ?? "saved";
  const lines = seed.patch.split("\n");
  const artifact: GitArtifact = {
    commit_sha: seed.sha, parent_commit_sha: seed.parent ?? null, committed_at: fixtureIso(seed.ago),
    subject: seed.subject, repository_url: seed.url, ref: seed.ref, provenance: "sample-dataset",
    patch_r2_key: `fixtures/${seed.sessionId}/${seed.sha}.patch`,
    patch_sha256: Array.from(digest, (byte) => byte.toString(16).padStart(2, "0")).join(""),
    patch_bytes: bytes.byteLength, patch_files: lines.filter((line) => line.startsWith("diff --git ")).length,
    patch_additions: lines.filter((line) => line.startsWith("+") && !line.startsWith("+++")).length,
    patch_deletions: lines.filter((line) => line.startsWith("-") && !line.startsWith("---")).length,
    capture_status: status, accepted_at: fixtureIso(seed.ago), saved_at: status === "saved" ? fixtureIso(seed.ago) : null,
    failed_at: status === "failed" ? fixtureIso(seed.ago) : null, failure_code: status === "failed" ? "patch_upload_failed" : null,
    created_at: fixtureIso(seed.ago),
  };
  return { sessionId: seed.sessionId, artifact, patch: seed.patch };
}));

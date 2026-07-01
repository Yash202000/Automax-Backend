import { danger, warn, fail, message } from "danger";

const modifiedFiles = danger.git.modified_files;
const createdFiles = danger.git.created_files;
const allChangedFiles = [...modifiedFiles, ...createdFiles];

// --- Rule 1: Bulk change warning ---
if (allChangedFiles.length > 15) {
  warn(
    `This PR changes **${allChangedFiles.length} files**. ` +
      `Large PRs are harder to review and more likely to introduce regressions. ` +
      `Consider splitting into smaller PRs.`
  );
}

// --- Rule 2: Critical file protection ---
const criticalFiles = [
  "internal/services/escalation.go",
  "internal/services/incident_service.go",
  "internal/repository/incident_repository.go",
  "internal/middleware/context.go",
  "internal/middleware/auth.go",
  "cmd/server/main.go",
  "docker-compose.yml",
  "Dockerfile",
];

const touchedCritical = allChangedFiles.filter((f) =>
  criticalFiles.some((cf) => f.endsWith(cf))
);

if (touchedCritical.length > 0) {
  warn(
    `This PR modifies critical files that need careful review:\n` +
      touchedCritical.map((f) => `- \`${f}\``).join("\n")
  );
}

// --- Rule 3: Commented-out code detection ---
const checkForCommentedCode = async () => {
  const goFiles = allChangedFiles.filter((f) => f.endsWith(".go"));
  const diffs = await Promise.all(
    goFiles.map((f) => danger.git.diffForFile(f))
  );

  const filesWithCommentedCode: string[] = [];

  for (const diff of diffs) {
    if (!diff) continue;
    const addedLines = diff.added.split("\n").filter((l) => l.startsWith("+"));
    const commentedOutLines = addedLines.filter(
      (l) =>
        l.match(/^\+\s*\/\//) &&
        !l.match(
          /^\+\s*\/\/\s*(TODO|FIXME|NOTE|HACK|nolint|go:)/
        )
    );
    if (commentedOutLines.length > 3) {
      filesWithCommentedCode.push(diff.path ?? "unknown");
    }
  }

  if (filesWithCommentedCode.length > 0) {
    warn(
      `Detected significant commented-out code in:\n` +
        filesWithCommentedCode.map((f) => `- \`${f}\``).join("\n") +
        `\n\nCommented-out code can mask reverted fixes (e.g., the i18n script that reverted the SLA fix). Please verify these are intentional.`
    );
  }
};

// --- Rule 4: Unrelated file changes ---
const goChanges = allChangedFiles.filter((f) => f.endsWith(".go"));
const nonGoChanges = allChangedFiles.filter(
  (f) => !f.endsWith(".go") && !f.endsWith(".mod") && !f.endsWith(".sum")
);

const serviceChanges = goChanges.filter((f) => f.includes("/services/"));
const handlerChanges = goChanges.filter((f) => f.includes("/handlers/"));
const modelChanges = goChanges.filter((f) => f.includes("/models/"));
const repoChanges = goChanges.filter((f) => f.includes("/repository/"));

const nonEmptyCategories = [
  serviceChanges,
  handlerChanges,
  modelChanges,
  repoChanges,
  nonGoChanges,
].filter((arr) => arr.length > 0);

if (nonEmptyCategories.length >= 4 && allChangedFiles.length > 10) {
  warn(
    `This PR touches files across many layers (services, handlers, models, repository, config). ` +
      `Ensure all ${allChangedFiles.length} changes are related to the PR's purpose.`
  );
}

// --- Rule 5: Bulk script detection ---
const prTitle = danger.github.pr.title.toLowerCase();
if (
  (prTitle.includes("i18n") ||
    prTitle.includes("lint") ||
    prTitle.includes("format") ||
    prTitle.includes("refactor")) &&
  allChangedFiles.length > 10
) {
  warn(
    `This appears to be a bulk operation PR (${allChangedFiles.length} files). ` +
      `Bulk scripts have previously reverted intentional fixes (e.g., i18n_transform.py reverting the SLA email fix). ` +
      `**Review each file's diff individually** before approving.`
  );
}

// --- Rule 6: Missing tests ---
const srcChanges = goChanges.filter((f) => !f.includes("_test.go"));
const testChanges = goChanges.filter((f) => f.includes("_test.go"));

if (srcChanges.length > 3 && testChanges.length === 0) {
  message(
    `This PR modifies ${srcChanges.length} Go source files but includes no test changes.`
  );
}

// Run async checks
checkForCommentedCode();

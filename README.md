# ai-reviewer

`ai-reviewer` reviews a pull request once, when GitHub sends `pull_request.opened`. It selects active rules by the repository's configured categories, asks Claude for structured findings, validates them against the diff, and publishes informational comments. SQLite stores the catalog, queue, attempts, immutable rule inputs, findings, prompt revisions, mining evidence and feedback.

## Setup

Copy [`examples/config.yml`](examples/config.yml) and authenticate the configured model provider. With `github.auth: app`, set the App ID and private-key path; a fixed installation ID is no longer required. Configuration version 1 is rejected with a migration message.

The administration panel lets you select a separate judge model. Choosing **Jev (TypeSafe AI)** uses `jev-latest` only to filter generated findings as accepted, duplicate, low-value, rejected, or uncertain; it is not available as a generation model. Set `TYPESAFE_API_KEY` in the reviewer process environment before selecting it. Only explicitly accepted findings are published.

```sh
make build
export GITHUB_WEBHOOK_SECRET='...'
bin/ai-reviewer doctor --config config.yml
bin/ai-reviewer serve --config config.yml
bin/ai-rules serve --config config.yml
```

Expose only the reviewer's `/webhooks/github` endpoint through HTTPS. Configure the GitHub App webhook for **Pull requests**, using the same secret. GitHub sends **Installation** and **Installation repositories** to every GitHub App automatically; they do not appear as selectable subscriptions. Installing the App automatically adds every selected repository to SQLite, enables reviews with the `COMMON` category, and makes it appear in the panel. Future `opened` pull requests are queued immediately; drafts wait for `ready_for_review`. Removing repository access or uninstalling the App disables reviews while preserving history. Each job mints a token for its own installation, so installations from different users and organizations work without per-repository setup.

Manual review observes the same uniqueness rule. Dry-run neither publishes nor creates a completed job:

```sh
bin/ai-reviewer review --repo example/backend --pr 123 --config config.yml
bin/ai-reviewer review --repo example/backend --pr 123 --dry-run --config config.yml
```

Maintenance is a finite command. Run `ai-rules sync --if-due` daily from `launchd` on macOS or a systemd timer; the persisted successful run determines whether 14 days elapsed. Historical initialization is explicit: `ai-rules bootstrap --all-history`. Inline human review comments are mined; replies and general PR comments are excluded by default. Three distinct comments promote a candidate. Three distinct GitHub 👎 reaction IDs disable a rule globally. Reactivation does not forgive old votes.

SQLite uses WAL, foreign keys and a busy timeout. Keep all writers and the DB on one host. Before moving it, stop writers, make a consistent SQLite backup, copy configuration and DB, authenticate `gh` and the model provider on the destination, update the webhook URL, then restart. GitHub deliveries missed while the machine or tunnel was offline must be inspected and redelivered; manual review remains idempotent.

The panel has tabs for rules, repositories, the global review prompt, and repository prompt overrides. Repository review/mining flags and category selections are stored in SQLite; YAML seeds repositories only on their first import and does not overwrite later web edits. Prompt changes are versioned with optimistic concurrency. Rule deletion and Markdown publication are absent. Prompt precedence is repository override, imported repository default, global override, then the generic product default. Existing review attempts retain the prompt and rules captured when they started.

No reviewed repository code is executed. Claude CLI runs in a controlled temporary directory with tools disabled. A failed CLI request never falls back to the paid API.

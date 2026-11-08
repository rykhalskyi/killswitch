# Project Wiki Schema

A project knowledge base stored in `wiki/`, version-controlled with the code. It tracks the project overview, build/deploy instructions, work items, decisions, and research.

Design goal: token-lean. Git is the change history. `AGENTS.md` is short and always loaded; this file holds the detail and is read on demand.

## Token discipline

1. **Git is the history.** Never duplicate it: no `## Updates` sections, no "Created <date>" in index entries, no restating superseded content. Use `git log -p wiki/pages/<file>`.
2. **One page per work item by default**, carrying `## Spec`, `## Plan`, `## Outcome`.
3. **Decisions get a one-liner** in `decisions.md`, referenced by id (`D-07`).
4. **Minimal frontmatter:** `created`, `type`, `status`, `summary`. No `tags`, no `related`.
5. **Never hand-edit `index.md`.** Regenerate with `node scripts/wiki.mjs index`. Append to `log.md` / `decisions.md` with the script.
6. **Keep `AGENTS.md` short.** Detail lives here.
7. **Batch writes** at the end of a work session.

## Layout

```
wiki/
├── AGENTS.md      # short, always-loaded summary
├── SCHEMA.md      # this file
├── index.md       # generated catalog — never edit by hand
├── log.md         # append-only activity log
├── decisions.md   # append-only decision record (ADR-lite)
└── pages/
    ├── project-overview.md
    ├── build-deploy.md
    ├── debugging.md          # optional
    ├── epics/
    ├── tickets/
    ├── specs/                # large/ambiguous epics only
    ├── plans/                # large/ambiguous epics only
    └── research/
```

The helper script lives at `scripts/wiki.mjs`.

## Frontmatter

```yaml
---
created: YYYY-MM-DD
type: overview | build-deploy | epic | ticket | spec | plan | research
status: proposed | in-progress | implemented | superseded   # when meaningful
summary: One-line description used to build index.md
---
```

- `created` never changes.
- `summary` is required so `index` works without reading bodies.
- No `tags` or `related`.

## Links

Relative markdown links, never wikilinks. Paths are relative to the file containing the link.

| From | To | Link |
|---|---|---|
| `wiki/index.md` | any page | `pages/<folder>/<slug>.md` |
| `pages/<sub>/x.md` | `pages/<other>/y.md` | `../<other>/y.md` |
| `pages/<sub>/x.md` | `pages/root-page.md` | `../root-page.md` |
| `pages/root-page.md` | `pages/<folder>/y.md` | `<folder>/y.md` |

## Slugs

- Tickets: `<ticket-id>-<brief>` (e.g. `ticket-15-localization-infra`, or `issue-11-dual-price`). Brief = 2-4 lowercase hyphenated words.
- Specs and plans share one `<feature-slug>`.
- Research: `<topic>`, lowercase hyphenated.

## Workflow

### Default: one page per work item

```markdown
# T<n> — <Title>

Part of the [epic](../epics/<slug>.md).   # if applicable

## Spec
What and why: requirements, scope, design decisions, constraints.

## Plan
Ordered steps, files to create/modify, migrations, tests, risks.

## Outcome
What actually shipped, deviations, files changed.
```

Flip `status:` to `implemented` when done.

### Large epic: optional spec + plan

Only for genuinely large/ambiguous work, or one spec shared by several tickets: `pages/specs/<slug>.md` and `pages/plans/<slug>.md`, cross-linked to the tickets.

## Operations

### Init

1. Create the `wiki/` structure.
2. Scan the project to populate `project-overview.md` and `build-deploy.md`.
3. Write short `AGENTS.md` and full `SCHEMA.md`.
4. Copy `wiki.mjs` to `scripts/wiki.mjs`; wire a `wiki` script if the project has an npm package.
5. Create `decisions.md` (on first use) and `log.md` with an init entry.
6. Run `node scripts/wiki.mjs index`.

Init is idempotent: never overwrite existing pages.

### Ticket

1. Create `pages/tickets/<slug>.md` with `created`, `type: ticket`, `status`, `summary`.
2. Add title, epic/spec/plan links, then `## Spec`, `## Plan`, `## Outcome`.
3. `node scripts/wiki.mjs index`.
4. `node scripts/wiki.mjs log ticket "<title>"`.

### Spec / Plan

1. Create `pages/specs/<slug>.md` / `pages/plans/<slug>.md` with `summary`.
2. Cross-link spec, plan, and tickets.
3. Regenerate index; log with `wiki.mjs log spec|plan "<name>"`.

### Research

1. Create `pages/research/<slug>.md` with `summary`: context, findings, conclusions, recommended actions.
2. Regenerate index; log with `wiki.mjs log research "<topic>"`.

### Decision

```
node scripts/wiki.mjs decision "<decision> — because <reason>"
```

Appends `- [YYYY-MM-DD] D-nn — <decision> — because <reason>` to `decisions.md`. Reference `D-nn` from the relevant ticket.

### Index / Log / Lint / Query / Update

- `node scripts/wiki.mjs index` — rewrite `index.md` from page frontmatter.
- `node scripts/wiki.mjs log <action> "<description>"` — append to `log.md`. Actions: `init`, `ticket`, `spec`, `plan`, `research`, `update`, `decision`.
- `node scripts/wiki.mjs lint` — missing frontmatter, dangling links, unindexed pages.
- Query: read `index.md` first, drill into pages, answer with relative links. Offer to file valuable answers as a research page.
- Update: edit the page directly; no `## Updates` section. Regenerate the index if the summary changed; log with `wiki.mjs log update "<page> — <note>"`.

## Migration from `.design-specs`

If `.design-specs/` or `.opencode/plans/` exists, offer to migrate:

1. `.design-specs/specs/*` → `pages/specs/<slug>.md` (file mtime as `created`).
2. `.opencode/plans/*` → `pages/plans/<slug>.md`.
3. Run `wiki.mjs index`; log each migration.
4. Do not delete originals unless explicitly asked.

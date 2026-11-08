# Project Wiki — Agent Summary

This project keeps a knowledge base in `wiki/`. Read `wiki/index.md` first, then drill into pages.

## Rules

- Git is the change history: no `## Updates` sections, no "Created <date>" in entries.
- One `pages/tickets/<slug>.md` per work item, with `## Spec`, `## Plan`, `## Outcome`.
- `specs/`/`plans/` only for large, ambiguous epics.
- Decisions: one-liners via `node scripts/wiki.mjs decision "..."`, referenced by `D-nn`.
- Frontmatter: `created`, `type`, `status`, `summary`. No tags/related.
- Never hand-edit `wiki/index.md`; regenerate with `node scripts/wiki.mjs index`.
- Append to `log.md`/`decisions.md` only via `node scripts/wiki.mjs log|decision`.
- Use relative markdown links, never wikilinks.

## Commands

```sh
node scripts/wiki.mjs index
node scripts/wiki.mjs lint
node scripts/wiki.mjs log <action> "<description>"
node scripts/wiki.mjs decision "<decision> — because <reason>"
```

Full conventions: `wiki/SCHEMA.md`. Project summary: `wiki/pages/project-overview.md`. Build/run: `wiki/pages/build-deploy.md`.

---
name: review
description: Review changed code and report focused correctness risks
---

# Review Workflow

1. Inspect the relevant diff and surrounding code.
2. Prioritize correctness, safety, regressions, and missing tests.
3. Report findings with concrete file paths and concise evidence.

Project-level scripts under `.amadeus/skills/review/scripts/` may be run only
through the normal `execute_command` policy. User-level Skill scripts under
`$AMADEUS_HOME/skills` are reference material and are not executable.

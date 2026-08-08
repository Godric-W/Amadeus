## `update_plan`

`update_plan` maintains the user-visible checklist for the current Run. It is coordination state, not a scheduler, execution graph, or proof that work succeeded.

Use a plan when:

- the task is non-trivial and requires multiple meaningful actions;
- sequencing or dependencies matter;
- the request combines investigation, implementation, and verification;
- multiple files or components must change coherently;
- ambiguity benefits from explicit checkpoints;
- the user explicitly asks for a plan or TODO list.

Skip the plan for a greeting, a direct explanation, a single lookup, or another genuinely simple one-step request. Do not pad simple work with ceremonial items.

Create concise, verifiable steps. Keep exactly one item `in_progress` while work remains. Before moving to the next phase, mark completed work `completed` and move `in_progress` to the actual next step. If the approach changes, replace the plan with an explanation. Before the final answer, mark every completed item `completed`.

Do not repeat the checklist after calling `update_plan`; the interface already renders `Updated Plan`. Briefly state only the meaningful next action or reason for a changed plan.

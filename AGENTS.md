# SoundDock agent instructions

## Subagents

When launching subagents (Task / explore / shell / review / any other subagent), use **only** this model:

- Display name: Cursor Grok 4.6 High Fast
- Model slug: `cursor-grok-4.6-high-fast`

Do not use Inherit, Composer, Grok 4.5, or any other agent model. Do not substitute a different model if this one is listed as available.

Pass `model: "cursor-grok-4.6-high-fast"` on every Task call.

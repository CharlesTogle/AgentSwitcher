# Agent Switcher

MVP Go TUI for switching between `codex`, `claude`, `gemini`, and `pi`.

## What it does

- Shows a terminal home screen with the four agents.
- Starts a new SQLite-backed session for the selected agent.
- Reopens recent sessions by UUID.
- Sends each prompt through the selected CLI in non-interactive mode.
- Shows the user prompt immediately and renders an in-chat `Thinking...` placeholder while the backend CLI is running.
- Persists prompts and replies in `agentswitcher.db`.
- Lets each session attach Markdown standards from a chosen directory.
- Compacts sessions every 12 user prompts by asking the same agent to summarize the accumulated conversation to 600 words.

## Current flow

1. Choose an agent from the home screen.
2. Press `Enter` to start a new session.
3. Press `Tab` to focus the recent sessions list and `Enter` to reopen one.
4. In chat, type a prompt and press `Ctrl+S` to send it.
5. Press `Ctrl+T` to open the standards picker for the current session.
6. In the standards picker, type a directory path, press `Tab` to autocomplete directories, press `Enter` to load Markdown files, use `Space` to toggle files, and press `Enter` again to save.
7. Press `Esc` to return to the home screen.

## Agent execution

This MVP uses the local CLIs as backends:

- `codex exec "<prompt>" --skip-git-repo-check --color never`
- `claude -p "<prompt>"`
- `gemini -p "<prompt>"`
- `pi -p "<prompt>"`

The app owns continuity itself by rebuilding the prompt from:

- selected standards documents
- the saved compaction summary
- the most recent stored messages
- the new user prompt

## Notes

- Responses are not streamed yet. The UI shows an optimistic conversation view plus a `Thinking...` placeholder while the backend call runs.
- Compaction uses a fixed threshold of 12 prompts, which sits inside your requested 10-15 range.
- Selected standards are reapplied on every prompt and are explicitly excluded from compaction summaries.
- The SQLite database is created in the working directory as `agentswitcher.db`.

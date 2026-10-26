If task is for Plan.md is implemented, after complete implementation delete that file. strictly no comments in code, don't hold back in ui logic, styling, and implementation logic

## Product focus

The webapp (`ui/` web build served by jackketchd TCP + `jack-ketch web`) is the only interface. Build web-first; keep every change working for web.

The TUI (`jack-ketch tui` dashboard and setup wizard) and the desktop GUI (`jack-ketch gui`, Wails shell) have been removed.

## Target CLI UX

- `jack-ketch` -> prints the command help.
- `jack-ketch web` -> prints the http link to the web interface and opens the browser.

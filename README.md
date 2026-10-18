# linear-tui

A keyboard-driven terminal UI for [Linear](https://linear.app), built with
[Bubble Tea v2](https://github.com/charmbracelet/bubbletea), Lip Gloss,
Bubbles, Huh and Glamour.

- **Issues**: list, filter, group, sort, bulk-edit, create, edit, archive, delete
- **Issue detail**: rendered markdown, sub-issues, relations, links, threaded comments
- **Board**: kanban by workflow state, move cards with `<` / `>`
- **Cycles & projects**: progress, milestones, issues per cycle/project
- **Inbox**: read/unread, snooze, archive, mark all read, unread badge
- **Search & command palette**: `ctrl+f` server-side search, `ctrl+k` for everything
- **Git**: copy or check out the issue branch, open the issue for the current branch
- **Auth**: personal API key or OAuth2 (PKCE), stored in the OS keyring

## Install

```sh
go install github.com/andrinoff/linear-tui/cmd/linear-tui@latest
```

Requires Go 1.27+. Prebuilt binaries are published on GitHub releases.

## Log in

The quickest way is a personal API key (Linear → Settings → Security & access →
Personal API keys):

```sh
export LINEAR_API_KEY=lin_api_...   # used as-is, never stored
# or store it in the OS keyring:
linear-tui auth login
```

If no credentials are found, the TUI shows a login screen.

### OAuth

1. Create an OAuth application in Linear (Settings → API → OAuth applications)
   with the redirect URI `http://127.0.0.1:<port>/callback`.
2. Set the client id and port in your config (or `LINEAR_OAUTH_CLIENT_ID`):

   ```toml
   [oauth]
   client_id = "your-client-id"
   port = 8976
   ```

3. Run `linear-tui auth login --oauth`, or choose OAuth on the login screen.

The flow uses PKCE, so no client secret is needed. Tokens refresh automatically.

Other auth commands: `linear-tui auth status` and `linear-tui auth logout`.
Credentials live in the OS keyring. If the keyring is unavailable (for example on
headless Linux), they go in a `0600` file in the config directory.

## Usage

```sh
linear-tui            # opens your issues (or the issue for the current git branch)
linear-tui ENG-123    # opens a specific issue
linear-tui --no-branch
```

### Keys

| Global | |
|---|---|
| `ctrl+k` / `:` | command palette |
| `ctrl+f` | search |
| `g i` `g m` `g t` `g b` `g c` `g p` `g v` `g s` | inbox, my issues, team issues, board, cycles, projects, views, search |
| `c` | new issue (prefilled from context) |
| `r` | refresh |
| `tab` | focus the sidebar or main area |
| `[` | toggle sidebar |
| `?` | all shortcuts |
| `q` / `esc` | back (quit at root) |
| `ctrl+c` | quit |

| Lists | |
|---|---|
| `ctrl+b` | switch between list and board (remembered per page) |
| `t` / `T` | next / previous tab (Assigned, Created, Subscribed…) |
| `j` / `k`, `enter` | move, open |
| `/` | filter loaded rows |
| `f` / `F` | add filter / clear filters |
| `v` / `S` | cycle grouping / sorting |
| `x` | select for bulk actions |

| Issue actions (list, board, detail) | |
|---|---|
| `s` `a` `A` `p` `L` `C` `P` | status, assignee, assign me, priority, labels, cycle, project |
| `e` / `m` | edit / comment |
| `y` `Y` `b` | copy identifier, URL, branch name |
| `o` | open in browser |
| `B` | `git checkout` the issue branch (creates it if needed) |
| `R` / `U` | add relation / link URL |
| `#` / `D` | archive / delete |

| Board | Detail | Inbox |
|---|---|---|
| `h` `l` columns, `<` `>` move card, `.` show canceled and older done | `n` / `N` next/prev link, `enter` follow or reply | `u` read, `z` snooze, `d` archive, `M` mark all read, `l` / `h` move into / out of the issue preview |

## Configuration

`config.toml` lives in `$XDG_CONFIG_HOME/linear-tui/` (macOS:
`~/Library/Application Support/linear-tui/`). See
[`config.example.toml`](config.example.toml) for every option, including key
overrides.

### Themes

The built-in theme follows Linear's palette and leaves the background
transparent. To make your own, add `themes/<name>.toml` to the config directory
and set `theme = "<name>"`. Keys you leave out fall back to the built-in
palette:

```toml
dark = true
monochrome = true     # gray labels/projects/teams; status icons and avatars keep color
background = ""       # "" is transparent; set a hex color to paint it
panel = ""
card = "#161616"
card_focus = "#262626"
chip = "#222222"
selection = "#232323"
accent = "#FFFFFF"
text = "#C4C4C4"
muted = "#8A8A8A"
border = "#2E2E2E"
```

The other keys are `chip_focus`, `modal`, `bright`, `subtle`, `red`, `orange`,
`yellow`, `green` and `blue`.

### Images

In kitty and Ghostty, avatars and the workspace logo are drawn as real images
through the kitty graphics protocol (Unicode placeholders). Elsewhere they fall
back to initials. Use `images = "on"` or `"off"` to override detection. To see
why an image isn't shown, run with `LINEAR_TUI_DEBUG=/tmp/linear-tui.log`.

### GitHub pull requests

Pull requests open in a Linear-style view: an Overview tab with the description
and activity (commits, force-pushes, review requests, reviews and comments), a
Diff tab (`t`, with `]` / `[` to jump between files), and a side panel showing
status, branch, reviewers, checks and changed files. You'll find them in the
inbox (PR notifications) and in an issue's links (press `enter` on a GitHub PR
link).

The GitHub token comes from `GH_TOKEN` or `GITHUB_TOKEN`, or else from the
GitHub CLI (`gh auth token`, so run `gh auth login` once). GitHub Enterprise
hosts work through `gh auth token --hostname`. In the PR view, `m` comments,
`B` runs `gh pr checkout`, and `o` opens the PR in the browser.

## Development

```sh
make test        # unit + TUI tests against a fake Linear API
make generate    # regenerate the typed GraphQL client (genqlient)
make schema      # refresh Linear's schema, then regenerate
make lint
```

GraphQL operations are in `internal/linear/queries/*.graphql`. The generated
client `internal/linear/generated.go` is committed, and CI checks that it is up
to date.

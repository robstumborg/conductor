# conductor

`conduct` is a CLI for running AI-assisted work inside a git repo without turning your terminal into soup.

Each task gets its own branch, worktree, tmux window, and agent session. Clean lane in, clean lane out.

## Dependencies

- Go 1.24+, 
- git
- tmux
- an agent CLI. current default setup uses `opencode` with `openai/gpt-5.4`.

## Install

```bash
make build
sudo make install
```

That gives you:

- `conduct` in `/usr/local/bin`
- `conduct(1)` in `/usr/local/share/man/man1`

You can override the install root if needed:

```bash
make install PREFIX=/opt/conductor
make install DESTDIR=/tmp/package-root
```

## Quick start

Inside a git repo:

```bash
conduct init
```

That creates `.conduct/`, writes a default config if needed, and ignores conductor runtime files.

Create a task and start it right away:

```bash
conduct new -t "Fix flaky tests" --start
```

No title yet? Create a new draft and open in your editor:

```bash
conduct new
```

Create a task with a title and then start it:

```bash
conduct new -t "Change button label 'Export CSV' to 'Export transactions'"
conduct start 1
```

Check what is active:

```bash
conduct status
conduct list
conduct show 1
```

Jump back into a running task window:

```bash
conduct open 1
```

When the task branch is committed and the task worktree is clean, land it from your main branch checkout:

```bash
conduct land 1
```

That applies a squash merge, archives the task, and cleans up the branch, worktree, and tmux window. You then review the final result and make the final commit yourself.

## Workflow

When you start work, `conduct` will:

- create or reuse a task branch like `conduct/0001-fix-flaky-tests`
- create a git worktree under `.conduct/worktrees/`
- write the active assignment to `.conduct/current.md` inside that worktree
- create a tmux window for the task
- launch your configured agent command in that window

If you are already inside tmux, the task starts in the background and `conduct` prints the target window. Otherwise it opens the task window for you.

## Customize and check

Enable shell completion:

```bash
# bash
source <(conduct completion bash)

# zsh
source <(conduct completion zsh)

# fish
conduct completion fish | source
```

See the effective config:

```bash
conduct config show
```

Check your environment:

```bash
conduct doctor
```

The default config looks like this:

```yaml
project:
  main_branch: main
agent:
  command: opencode
  args:
    - --model
    - "{model}"
    - --prompt
    - "{prompt}"
  default_model: openai/gpt-5.4
tmux:
  session_prefix: conduct
```

## Reference

Use the man page:

```bash
man conduct
```

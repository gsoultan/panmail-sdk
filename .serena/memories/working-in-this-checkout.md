# Working in this checkout

- The owner edits and commits in this same working tree while a session runs.
  Re-check `git status` and `git log` before committing or describing repo state;
  stage explicit paths, never `git add -A`, and commit promptly rather than
  leaving work staged across turns.
- Feature work goes on a branch with a pull request, which the owner merges —
  squash-merged, as #14 and #15 were. After pushing, switch the checkout back
  to `main`, so the owner's next commit lands where they expect rather than on
  the feature branch. Pushing a pull request's commits straight into `main`
  from a session was refused by auto mode as a merge without review.
- `.serena/` and `graphify-out/` are committed. graphify's build state is not —
  `graphify-out/cache/`, `manifest.json` and `.graphify_root` are in
  `.gitignore`, because they hold mtimes, this machine's absolute path and a
  stamp that moves on every query.
- The knowledge graph was first built with `graphify extract . --code-only` and
  `graphify cluster-only . --no-label` (no LLM call). Refresh it with
  `graphify update .` once code lands on `main`, commit the result, and
  re-export with `graphify export obsidian --dir
  ~/Documents/ObsidianVault/Panmail-SDK`, which also prunes notes for symbols
  that no longer exist.
- Commit messages: `type: lowercase summary`, then prose paragraphs that explain
  why. No trailers of any kind.

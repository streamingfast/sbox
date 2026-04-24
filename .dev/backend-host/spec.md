Let's add a `--backend=host` so that sbox runs locally (no entrypoint but supports sbox loop and bypass permission)

  Questions:
  1. Does "no entrypoint" mean the agent runs directly on the host with no Docker at all?
  Correct and that what the whole `host` will mean (unless you have a better name idea)
  2. For "supports sbox loop" — should the loop run in the sbox process itself on the host?
    It would have the loop behavior but instead of invoking a sandbox, it launch on the host the agent process
  3. "Bypass permission" — inject --dangerously-skip-permissions or something else?
    Like we have right now for any other backend which is bypass all permission
  4. Should sbox stop/sbox shell/sbox info work for host backend?

     No those should clearly explain to the user that those command and not supported and exit with 1

  5. Should --profile be silently ignored for host?

        Should show a warning that it's not supported when backend is host
  6. Should sbox still write .sbox/ dir for host mode?

    Yes we can inject some global instructions too.

## Feedback 1

```
	// Collect plugin directories for --plugin-dir flags
	pluginDirs := hostCollectPluginDirs(opts.WorkspaceDir, agentType)
```

There should be no plugin on backend host, since those are pulled from the host already.
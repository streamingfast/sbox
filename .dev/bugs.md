- sandbox instructions for firewall not working

  When the sandbox detects it needs some firewall access, the constructed command had the wrong sandbox name:

  ```
  It looks like I need to request network access to github.com from you. Could you run this command to allow network access?
docker sandbox network proxy firehose-core --allow-host github.com --allow-host raw.githubusercontent.com
```

  Here the sandbox `firehose-core` is wrong and it was `sbox-opencode-firehose-core`. This is derived from instructions injected in the sandbox/container.

  Instructions needs to be tweaked so that the correct sandbox name is generated and clearer formatting output should be suggested to agent wrapping it with backtick ` for proper "cutting" of arguments when selecting.
## Feature

When running multiple sbox, especially in backend=container, for some larger project, it can be fast that file open limits get exhausted due to how Docker sync filesystems.

Similar to `sbox prune`, we want a way to stop least recently used sbox (regargless of sandbox/container/host distinction I think for now.)

I'm thinking we should `sbox stop all [--force]` that would heave similar semantic behavior as prune but would simply stop the sbox and its container/sandbox without deleting anything here.

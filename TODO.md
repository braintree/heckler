# Todo

## Next Up

- [ ] Rename AutoClose to AutoApprove
- [ ] Slack notifications
- [ ] Highlight Service Refreshes in GitHub issue
- [ ] Hot Fix Support

  Hot fixing v2 to v2.1 works at present, but hot fixing v2.1 from v1.0 would
  require applying v2.0 first, before applying v2.1. This might be unwanted if
  there is known breakage in v2.0.

- [ ] Additional Tests
  - [ ] Git graphs
    - [ ] Branch without a common ancestor to last applied commit, i.e. the
      last applied commit is ahead of our common ancestor
    - [ ] If the last apply commit is not an ancestor of master, but is on
      branch, hecklerd should refuse to puppet
- [ ] ︙

## Known Bugs

- [ ] Applying an unknown rev panics hecklerd
- [ ] Serialized reports grow stale: hecklerd could track the aggregate node
      status as a hash, similar to go.sum, and renoop if the status has
      changed?

## Nice to Have

- [ ] IgnoredResources should be a map with the value and a reason for the ignore
  rule
- [ ] Tune timeouts
- [ ] Heckler cli should respect sudoers, i.e. if you don't have root, you
  can't apply, otherwise heckler cli should only be accessible by root.
  How do prove who you are? Use HTTP Signatures via ssh keys?
      
    - https://tools.ietf.org/html/draft-ietf-httpbis-message-signatures-00,
    - Go implementation: https://github.com/go-fed/httpsig

## Development Sugar

- [ ] Upstream git cgi commits
- [ ] GitHub clear should handle pagination, and clear all the issues
- [ ] Break up into libraries
- [ ] Move libgit2 build into the docker image?
      we already build musl and LibreSSL in the container, this would speed up
      build times

## Application Sugar

- [ ] Status command should reverse rev-parse, i.e. oid to master and tags?
- [ ] Heckler status command should include lock status?
- [ ] Change heckler arg parsing to use github.com/spf13/cobra?
- [ ] Graceful shutdown
  - [ ] Cancel all contexes
  - [x] Unlock all nodes
- [ ] Send permadiffs to server owners??
- [ ] Rizzod crash recovery of git repo
  - [ ] Remove index.lock
  - [ ] Git checkout; git clean -fd

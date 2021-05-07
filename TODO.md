# Todo

## Next Up

-   [ ] Should rizzod unlock on startup?
-   [ ] Enforce GitHubAppEmail if autoTag enabled
-   [ ] Closed issues should be reopened if no longer approved
-   [ ] Stale Lock notifications, including reminders
-   [ ] Heckler should lock as itself as the heckler user
    -   [ ] ApplyFailures, should lock as root
-   [ ] Rename AutoClose to AutoApprove
-   [ ] Ignored resources **should** be present in the GitHub issue noop.
        Its confusing to the user as to why their commit didn't generate a
        diff, when in reality it did, but their diff does not need to be
        approved, so Heckler has hid it.
-   [ ] Highlight Service Refreshes in GitHub issue
-   [ ] Add labels to GitHub Issues
    -   [ ] Refactor issue searches to use labels
    -   [ ] Remove string labels in titles, e.g. production\_env
-   [ ] Additional Tests
    -   [ ] Git graphs
        -   [ ] Branch without a common ancestor to last applied commit,
            i.e. the last applied commit is ahead of our common ancestor
        -   [ ] If the last apply commit is not an ancestor of master,
            but is on branch, hecklerd should refuse to puppet
-   [ ] Apply Improvements
    -   [ ] Add apply set order to slack thread on each apply
    -   [ ] Announce completion on thread with link to commits & errors
    -   [ ] If an apply set has already been applied, in other words all
        nodes are beyond the current rev, skip to next set without
        waiting. This would save time on reapplying an interrupted
        apply.
-   [ ] Improve ParentEvalErrors: We currently suppress the noop since
    we can't create a delta noop, but this is confusing for commits
    which fix the eval error
    -   [ ] Link to full diff?
-   [ ] tagging tool, ruminate on the best solution, is autotagging
    sufficient?
-   [ ] ︙

## Known Bugs

-   [ ] Applying an unknown rev panics hecklerd
-   [ ] Escape codes are still present in console error logs with
    `--color=false`. There is a trailing `^[[0m` still present, is this
    due to our custom color patch for Puppet?
-   [ ] Diffs are cumulative when multiple commits change a single file,
    since Heckler does not know how to separate out the changes from
    each commit.
-   [ ] Output is trimmed when the issue body exceeds the length limit
    of 65536

## Nice to Have

-   [ ] Heckler cli should respect sudoers, i.e. if you don't have root,
    you can't apply, otherwise heckler cli should only be accessible by
    root. How do prove who you are? Use HTTP Signatures via ssh keys?
    -   <https://tools.ietf.org/html/draft-ietf-httpbis-message-signatures-00>,
    -   Go implementation: <https://github.com/go-fed/httpsig>

## Development Sugar

-   [ ] Upstream git cgi commits
-   [ ] GitHub clear should handle pagination, and clear all the issues
-   [ ] Break up into libraries
-   [ ] Move libgit2 build into the docker image? We already build musl
    and LibreSSL in the container, this would speed up build times.

## Application Sugar

-   [ ] Heckler status command should include lock status?
-   [ ] Change heckler arg parsing to use github.com/spf13/cobra?
-   [ ] Graceful shutdown
    -   [ ] Cancel all contexts
    -   [x] Unlock all nodes
-   [ ] Send permadiffs to server owners??

# Heckler

Heckler's aim is to allow you to correlate code changes with Puppet noop
output. Heckler runs a Puppet noop on every host for every new commit in a git
repository. Heckler then ties together each noop change with the commit
responsible for the change. Heckler then allows you to review the actual
changes that will be made to a host before they are applied.

# Build

```
make build
```

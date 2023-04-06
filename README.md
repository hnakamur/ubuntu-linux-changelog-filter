# ubuntu-linux-changelog-filter

A filter CLI (command line interface) for Ubuntu Linux kernel changelog.

## How to install

Prerequisite: Install `go` command (See [The Go Programming Language](https://go.dev/)).

Run the following command to install this filter CLI:

```
go install github.com/hnakamur/ubuntu-linux-changelog-filter@latest
```

## How to use

Download a changelog file from http://changelogs.ubuntu.com/changelogs/pool/main/l/linux/

Run the following command to print the filtered changelog:

```
linux-changelog-filter@latest -file /path/to/changelog -filter your_filter_here
```

For syntax of regular expression for filter, see https://pkg.go.dev/regexp/syntax

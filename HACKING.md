# Hacking on Pebble

- [Running the daemon](#running-the-daemon)
- [Using the CLI client](#using-the-cli-client)
- [Using Curl to hit the API](#using-curl-to-hit-the-api)
- [Running the tests](#running-the-tests)
- [Creating a release](#creating-a-release)

Hacking on Pebble is easy. It's written in Go, so install or [download](https://golang.org/dl/) a copy of the latest version of Go. Pebble uses [Go modules](https://golang.org/ref/mod) for managing dependencies, so all of the standard Go tooling just works.

To compile and run Pebble, use the `go run` command on the `cmd/pebble` directory. The first time you run it, it will download dependencies and build packages, so will take a few seconds (but after that be very fast):

```
$ go run ./cmd/pebble
Pebble lets you control services and perform management actions on
the system that is running them.

Usage: pebble <command> [<options>...]
...
```

If you want to build and install the executable to your `~/go/bin` directory (which you may want to add to your path), use `go install`:

```
$ go install ./cmd/pebble
```

However, during development it's easiest just to use `go run`, as that will automatically recompile if you've made any changes.


## Running the daemon

To run the Pebble daemon, set the `$PEBBLE` environment variable and use the `pebble run` sub-command, something like this:

```
$ mkdir ~/pebble
$ export PEBBLE=~/pebble
$ go run ./cmd/pebble run
2021-09-15T01:37:23.962Z [pebble] Started daemon.
...
```


## Using the CLI client

The use the Pebble command line client, run one of the other Pebble sub-commands, such as `pebble plan` or `pebble services` (if the server is running in one terminal, do this in another):

```
$ export PEBBLE=~/pebble
$ go run ./cmd/pebble plan
services:
    snappass:
        override: replace
        command: sleep 60

$ go run ./cmd/pebble services
Service   Startup   Current
snappass  disabled  inactive
```


## Using Curl to hit the API

For debugging, you can also use [curl](https://curl.se/) in unix socket mode to hit the Pebble API:

```
$ curl --unix-socket ~/pebble/.pebble.socket 'http://localhost/v1/services?names=snappass' | jq
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
100   120  100   120    0     0   117k      0 --:--:-- --:--:-- --:--:--  117k
{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "name": "snappass",
      "startup": "disabled",
      "current": "inactive"
    }
  ]
}
```


## Running the tests

Pebble has a suite of Go unit tests, which you can run using the regular `go test` command. To test all packages in the Pebble repository:

```
$ go test ./...
ok      github.com/canonical/pebble/client  (cached)
?       github.com/canonical/pebble/cmd [no test files]
ok      github.com/canonical/pebble/cmd/pebble  0.095s
...
```

To test a single package, simply pass the package path to `go test`:

```
$ go test ./cmd/pebble
ok      github.com/canonical/pebble/cmd/pebble  0.115s
```

To run a single suite or a single test, pass the suite or test name to the [gocheck](https://labix.org/gocheck) test runner:

```
$ go test ./cmd/pebble -v -check.v -check.f PebbleSuite
=== RUN   Test
PASS: cmd_add_test.go:38: PebbleSuite.TestAdd   0.002s
PASS: format_test.go:52: PebbleSuite.TestCanUnicode 0.000s
...
PASS: cmd_version_test.go:26: PebbleSuite.TestVersionCommand    0.000s
OK: 20 passed
--- PASS: Test (0.02s)
PASS
ok      github.com/canonical/pebble/cmd/pebble  0.022s

$ go test ./cmd/pebble -v -check.v -check.f PebbleSuite.TestAdd
=== RUN   Test
PASS: cmd_add_test.go:38: PebbleSuite.TestAdd   0.002s
OK: 1 passed
--- PASS: Test (0.00s)
PASS
ok      github.com/canonical/pebble/cmd/pebble  0.007s
```

Note that during CI we run the tests with `-race` to catch data races:

```
$ go test -race ./...
ok      github.com/canonical/pebble/client  (cached)
?       github.com/canonical/pebble/cmd [no test files]
ok      github.com/canonical/pebble/cmd/pebble  0.165s
...
```


## Creating a release

To create a new tagged release, go to the [GitHub Releases page](https://github.com/canonical/pebble/releases) and:

- Click "Create a new release"
- Enter the version tag (eg: `v1.2.3`) and select "Create new tag: on publish"
- Enter a release title: include the version tag and a short summary of the release
- Write release notes: describe new features and bug fixes, and include a link to the full list of commits

Binaries will be created and uploaded automatically to this release by the [binaries.yml](https://github.com/canonical/pebble/blob/master/.github/workflows/binaries.yml) GitHub Actions job.

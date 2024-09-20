# Pebble Integration Tests

This directory holds a suite of integration tests for end-to-end tests of things like pebble run. They use the standard go test runner, but are only executed if you set the integration build constraint.

## Run Tests

```bash
go test -count=1 -tags=integration ./tests/
```

The above command will build Pebble first, then run tests with it.

To use an existing Pebble binary rather than building one, you can explicitly set the flag `-pebblebin`. For example, the following command will use a pre-built Pebble at `/home/ubuntu/pebble`:

```bash
go test -v -count=1 -tags=integration ./tests -pebblebin=/home/ubuntu/pebble
```

## Developing

### Visual Studio Code Settings

For VSCode Go and the gopls extention to work properly with files containing build tags, add the following:

```json
{
    "gopls": {
        "build.buildFlags": [
            "-tags=integration"
        ]
    }
}
```

# Pebble Integration Tests

This directory holds a suite of integration tests for end-to-end tests of things like pebble run. They use the standard go test runner, but are only executed if you set the integration build constraint.

## Run Tests

```bash
go test -count=1 -tags=integration ./tests/
```

## Developing

### Visual Studio Code Settings

For the VSCode Go and gopls extention to work properly with files containing build tags, add the following:

```json
{
    "gopls": {
        "build.buildFlags": [
            "-tags=integration"
        ]
    }
}
```

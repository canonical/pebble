# Pebble Integration Tests

## Run Tests

```bash
go test -tags=integration ./tests/
```

## Developing

### Clean Test Cache

If you are adding tests and debugging, remember to clean test cache:

```bash
go clean -testcache && go test -v -tags=integration ./tests/
```

### Visual Studio Code Settings

For the VSCode Go extention to work properly with files with build tags, add the following:

```json
{
    "gopls": {
        "build.buildFlags": [
            "-tags=integration"
        ]
    }
}
```

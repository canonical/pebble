# Pebble Integration Tests

## Run Tests

```bash
go test -count=1 -tags=integration ./tests/
```

## Developing

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

# How to check health based on logs

Need to be clear that this is not the primary way to check health. It's kind of a hack...

## Define a layer

```yaml
services:
  foo:
    override: replace
    command: foo
    startup: enabled
checks:
  foo-warning:
    override: replace
    threshold: 1
    exec:
      command: bash -c '! pebble logs | grep -q WARNING'
```

- `pebble check foo-warning`
- `pebble health foo-warning`

## See more

- [`pebble check`](#reference_pebble_check_command) command
- [`pebble health`](#reference_pebble_health_command) command
- [](/reference/health-checks)

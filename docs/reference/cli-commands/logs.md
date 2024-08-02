(reference_pebble_logs_command)=
# logs command

The Pebble daemon's service manager stores the most recent stdout and stderr from each service, using a 100KB ring buffer per service. Each log line is prefixed with an RFC-3339 timestamp and the `[service-name]` in square brackets.

## Usage

Logs are viewable via the logs API or using `pebble logs`:

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble logs --help
Usage:
  pebble logs [logs-OPTIONS] [<service>...]

The logs command fetches buffered logs from the given services (or all services
if none are specified) and displays them in chronological order.

[logs command options]
      -f, --follow     Follow (tail) logs for given services until Ctrl-C is
                       pressed. If no services are specified, show logs from
                       all services running when the command starts.
          --format=    Output format: "text" (default) or "json" (JSON lines).
      -n=              Number of logs to show (before following); defaults to
                       30.
                       If 'all', show all buffered logs.
```
<!-- END AUTOMATED OUTPUT -->

## Examples

To view logs, run:

```{terminal}
   :input: pebble logs
2022-11-14T01:35:06.979Z [srv1] Log 0 from srv1
2022-11-14T01:35:08.041Z [srv2] Log 0 from srv2
2022-11-14T01:35:09.982Z [srv1] Log 1 from srv1
```

To view existing logs and follow (tail) new output, use `-f` (press Ctrl-C to exit):

```{terminal}
   :input: pebble logs -f
2022-11-14T01:37:56.936Z [srv1] Log 0 from srv1
2022-11-14T01:37:57.978Z [srv2] Log 0 from srv2
2022-11-14T01:37:59.939Z [srv1] Log 1 from srv1
^C
```

You can output logs in JSON Lines format, using `--format=json`:

```{terminal}
   :input: pebble logs --format=json
{"time":"2022-11-14T01:39:10.886Z","service":"srv1","message":"Log 0 from srv1"}
{"time":"2022-11-14T01:39:11.943Z","service":"srv2","message":"Log 0 from srv2"}
{"time":"2022-11-14T01:39:13.889Z","service":"srv1","message":"Log 1 from srv1"}
```

If you want to also write service logs to Pebble's own stdout, run the daemon with `--verbose`:

```{terminal}
   :input: pebble run --verbose
2022-10-26T01:41:32.805Z [pebble] Started daemon.
2022-10-26T01:41:32.835Z [pebble] POST /v1/services 29.743632ms 202
2022-10-26T01:41:32.835Z [pebble] Started default services with change 7.
2022-10-26T01:41:32.849Z [pebble] Service "srv1" starting: python3 -u /path/to/srv1.py
2022-10-26T01:41:32.866Z [srv1] Log 0 from srv1
2022-10-26T01:41:35.870Z [srv1] Log 1 from srv1
2022-10-26T01:41:38.873Z [srv1] Log 2 from srv1
...
```

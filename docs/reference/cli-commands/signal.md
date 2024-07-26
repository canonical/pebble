(reference_pebble_signal_command)=
# signal command

The `signal` command is used to send a signal to one or more running services.

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
:input: pebble signal --help
Usage:
  pebble signal <SIGNAL> <service>...

The signal command sends a signal to one or more running services. The signal
name must be uppercase, for example:

pebble signal HUP mysql nginx
```
<!-- END AUTOMATED OUTPUT -->

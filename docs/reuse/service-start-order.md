Start: Service start order note

```{note}
By default, Pebble only waits 1 second before moving on to start the next service, with no additional checks that the previous service is operating correctly. To make a service wait until it is actually ready, define [latch checks](#latch-checks) in its `on-check-success` map.

If the configuration of `before` and `after` for the services results in a cycle, an error will be returned when the Pebble daemon starts (and the plan is loaded) or when a layer that causes a cycle is added.
```

End: Service start order note

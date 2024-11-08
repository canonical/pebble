Start: Service start order note

```{note}
Currently, `before` and `after` are of limited usefulness, because Pebble only waits 1 second before moving on to start the next service, with no additional checks that the previous service is operating correctly.

If the configuration of `before` and `after` for the services results in a cycle, an error will be returned when the Pebble daemon starts (and the plan is loaded) or when a layer that causes a cycle is added.
```

End: Service start order note

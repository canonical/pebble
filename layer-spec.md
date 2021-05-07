## Type: Layer

| Field       |         Type         | Default | Required? | Description                                                                                     |
| :---------- | :------------------: | :-----: | :-------: | :---------------------------------------------------------------------------------------------- |
| summary     |       `string`       |  `nil`  |    no     | Short one line summary of the layer                                                             |
| description |       `string`       |  `nil`  |    no     | A full description of the layer                                                                 |
| services    | `map[string]Service` |  `[]`   |    yes    | A map of services for Pebble to manage. The key represents the name of the service in the layer |

## Type: Service

| Field       |                       Type                        | Default | Required? | Description                                                                                                                             |
| :---------- | :-----------------------------------------------: | :-----: | :-------: | :-------------------------------------------------------------------------------------------------------------------------------------- |
| summary     |                     `string`                      |  `nil`  |    no     | Short one line summary of the service.                                                                                                  |
| description |                     `string`                      |  `nil`  |    no     | A full description of the service.                                                                                                      |
| startup     |  [`ServiceStartup`](#enumeration-servicestartup)  |  `nil`  |    no     | Value `enabled` will ensure service is started automatically when Pebble starts. Value `disabled` will do the opposite.                 |
| override    | [`ServiceOverride`](#enumeration-serviceoverride) |  `nil`  |    yes    | Control the effect the new layer will have on the current Pebble plan, choosing between `override` and `merge`                          |
| command     |                     `string`                      |  `nil`  |    yes    | Command for Pebble to run. Example `/usr/bin/myapp -a -p 8080`                                                                          |
| after       |                    `[]string`                     |  `nil`  |    no     | Array of other service names in the plan that should start before the service                                                           |
| before      |                    `[]string`                     |  `nil`  |    no     | Array of other service names in the plan that this service should start before                                                          |
| requires    |                    `[]string`                     |  `nil`  |    no     | Array of other service names in the plan that this service requires to be started before being started itself                           |
| environment |                `map[string]string`                |  `nil`  |    no     | A map of environment variable names, and their respective values, that should be injected into the environment of the service by Pebble |

## Enumeration: ServiceStartup

| Value      | Description                                               |
| :--------- | :-------------------------------------------------------- |
| `enabled`  | Start the service automatically when Pebble starts        |
| `disabled` | Do not start the service automatically when Pebble starts |

## Enumeration: ServiceOverride

| Value      | Description                                                                                                                       |
| :--------- | :-------------------------------------------------------------------------------------------------------------------------------- |
| `merge`    | Merge the specification of the service with any existing specification of a service with the same name in the current Pebble plan |
| `override` | Override any services of the same name in the current Pebble plan                                                                 |

# Timer string format

Timer strings are used for configuring service `schedule`s. They follow the
[schedule syntax](https://github.com/niemeyer/schedule-syntax), which selects **when** events happen by
combining three dimensions: days of the year, weekdays, and times of the day.

## Syntax

```
schedule = set *( ",," set )
set      = element *( "," element )
element  = months / days / weekdays / clock

months   = month [ "-" month ]
month    = "jan" / "feb" / "mar" / "apr" / "may" / "jun" /
           "jul" / "aug" / "sep" / "oct" / "nov" / "dec"
days     = day [ "-" day ]
day      = 1*2DIGIT                                 ; 1 to 31

weekdays = weekday [ "-" weekday ]
weekday  = wday [ pos ]
wday     = "sun" / "mon" / "tue" / "wed" / "thu" / "fri" / "sat"
pos      = "1" / "2" / "3" / "4" / "5"              ; 5 is the last

clock    = time / time "-" time [ "/" split ]
time     = 1*2DIGIT ":" 2DIGIT                      ; 0:00 to 24:00
split    = count [ "s" / "m" / "h" ]
count    = 1*5DIGIT                                 ; 1 to 99999
```

Clock times are always specified in 24H format, in the system's local time.

## Examples

* `0:00`</br>
  Every day at 0:00

* `0:00-24:00/1h`</br>
  Every hour on the hour

* `0:00-24:00/30m`</br>
  Every 30 minutes

* `12:00-13:00/5m`</br>
  Every 5 minutes from 12:00 to 12:55

* `8:00-10:00/3`</br>
  Every day at 8:00, 8:40 and 9:20

* `mon,10:00,,fri,15:00`</br>
  Mondays at 10:00, Fridays at 15:00

* `mon,fri,10:00,15:00`</br>
  Mondays at 10:00 and 15:00, Fridays at 10:00 and 15:00

* `mon-fri,8:00-17:00,,sat,sun,10:00,,mon1,9:00`</br>
  Workdays at 8:00, weekends at 10:00, and the first Monday of each month at 9:00

* `mon2-wed,23:00`</br>
  2nd Monday of the month through the following Wednesday, at 23:00

* `fri5,23:00`</br>
  Last Friday of the month, at 23:00

* `dec-mar,1-14,sat`</br>
  Saturdays that are among days 1 to 14 of December through March, at 0:00

* `dec,31,23:55`</br>
  New Year's Eve at 23:55

## Semantics

A timer string is composed of one or more sets, separated by a double comma (`,,`). Up to 8 sets are supported. The next event is the earliest one selected by any of the sets.

Within a set, elements are separated by a single comma. Each element is recognised by its shape, so elements may appear in any order, except that days of the month must follow the months they apply to. Up to 8 weekday spans and 8 clock spans are supported per set.

An event happens when a day matches the set's days of the year and one of its weekday spans, at a time selected by one of its clock spans. Anything omitted falls back to a default:

| Dimension | Default | Meaning |
| --- | --- | --- |
| Months/days | all year | every day of every month |
| Weekdays | `sun-sat` | every weekday |
| Time | `0:00-0:00` | the whole day, once a day at 0:00 |

### Days of the year

Months are three-letter names, and may form ranges that wrap around the year, so `nov-feb` is November through February.

Days of the month (1 to 31) may follow the months they apply to, either as single days or ranges. Day ranges wrap around the month, so `25-5` is days 25 to 31 and 1 to 5. Days apply to all the months listed right before them:

    dec-mar,5-10

This selects days 5 to 10 of each month from December through March. A month listed after days starts a new group of months, so the following selects January 15th to 20th and the whole of February:

    jan,15-20,feb

If no days are given for a month, all its days are selected. Days that don't exist in a month, such as `feb,30`, never match.

### Weekdays

Weekdays may form ranges that wrap around the week, so `fri-mon` is Friday, Saturday, Sunday and Monday.

A digit after a weekday restricts it to a position within the month, with `5` meaning the last one. For example, `mon1,mon3` is the first and third Mondays of each month, and `fri5` is the last Friday of each month, even in months with only four Fridays.

Weekday ranges may be positioned at either end:

    mon1-fri
    mon-fri1

The first example describes a span that starts on the first Monday of the month and ends on the following Friday, while the second example describes a span that starts on the Monday _before_ the first Friday of the month, which is when the span ends.

Consider the following calendar months of July and August 2019:

```
        July                  August
Su Mo Tu We Th Fr Sa   Su Mo Tu We Th Fr Sa
    1  2  3  4  5  6                1  2  3
 7  8  9 10 11 12 13    4  5  6  7  8  9 10
14 15 16 17 18 19 20   11 12 13 14 15 16 17
21 22 23 24 25 26 27   18 19 20 21 22 23 24
28 29 30 31            25 26 27 28 29 30 31
```

In the above context, `mon1-fri` describes a span from the 5th of August to the 9th of August, while `mon-fri1` covers the 29th of July until the 2nd of August.

### Times of day

Clock times are `H:MM` or `HH:MM`, with `24:00` accepted as midnight (the same as `0:00`).

Each clock span triggers a single event at its start. A single time is shorthand for a 15 minute span, so `9:45` is the span from 9:45 to 10:00. An explicit span is written with `-`, such as `8:00-17:00`. Spans may cross midnight, so `23:00-1:00` is from 23:00 to 1:00 the next day, and `0:00-0:00` (or `0:00-24:00`) is the whole day.

A span may be split into repeated events by appending `/N`. With no unit, `N` is the number of equal parts the span is divided into, with an event at the start of each part. With a unit (`s`, `m` or `h`), `N` is the period between events in seconds, minutes or hours:

    8:00-10:00/4
    8:00-10:00/30m

Both of these trigger events at 8:00, 8:30, 9:00 and 9:30. The end of a span is never itself an event.

An event that's due while its span (or part of a span) is still open is picked up straight away. For example, with a schedule of `9:00-11:00`, if a service's schedule is added at 10:00, the service is started straight away rather than at 9:00 the next day.

# Timer string format

Timer strings are used for configuring service `schedule`s.

See this [discourse thread](https://forum.snapcraft.io/t/refresh-scheduling-on-specific-days-of-the-month/1239/6) for details on how the syntax was conceived and evolved over time.

## Syntax

```
eventlist = eventset *( ",," eventset )
eventset = wdaylist / timelist / wdaylist "," timelist

wdaylist = wdayset *( "," wdayset )
wdayset = wday / wdaynumber / wdayspan
wday =  ( "mon" / "tue" / "wed" / "thu" / "fri" / "sat" / "sun" )
wdaynumber =  ( "sun" / "mon" / "tue" / "wed" / "thu" / "fri" / "sat" ) DIGIT
wdayspan = wday "-" wday / wdaynumber "-" wday / wday "-" wdaynumber
wspec = ( "1" / "2" / "3" / "4" / "5" )

timelist = timeset *( "," timeset )
timeset = time / timespan
time = 2DIGIT ":" 2DIGIT
timespan = time ( "-" / "~" ) time [ "/" count ]
count = n*DIGIT
```
Clock times are always specified in 24H format.

## Examples

* `00:00-24:00/24`</br>
   Every hour on the hour

*  `00:00-24:00/48`</br>
   Every 30 minutes

*  `00:00-24:00/96`</br>
   Every 15 minutes

* `12:00-13:00/12`</br>
   Every 5 minutes from 12:00 to 13:10

* `23:00`</br>
   Every day at 23:00

More specific timer examples:

* `mon,10:00,,fri,15:00`</br>
   Mondays at 10:00, Fridays at 15:10 

* `mon,fri,10:00,15:00`</br>
  Mondays at 10:00 and 15:00, Fridays at 10:00 and 15:00

* `mon-wed,fri,9:00-11:00/2`</br>
  Monday to Wednesday and on Friday, twice between 9:00 and 11:00 

* `mon,9:00~11:00,,wed,22:00~23:00`</br>
  Mondays, some time between 9:00 and 11:00, and on Wednesdays, some time between 22:00 and 23:00

* `mon,wed`</br>
  Monday and on Wednesday, at 0:00

* `mon2-wed,23:00-24:00`</br>
  2nd Monday of the month through the following Wednesday, between 23:00 and 24:00

* `fri5,23:00-01:00`</br>
  Last Friday of the month, from 23:00 to 1:00 the next day. Even in months with 4 Fridays, this schedule will still trigger on the last Friday.

##  Semantics

A timer string is composed of one or more event sets, which are combined by using commas (`,,`) as separators.

Each event set defines the weekdays and the time windows in which events may occur. The next event will be scheduled inside the soonest opportunity that matches both one of the provided weekdays and one of the provided time windows. If no weekdays are provided, the default is every day. If no time windows are provided, the default is an arbitrary time in the day.

For example, consider the timer:

    mon,fri,10:00,15:00

Assuming today is Sunday, the next 5 events are, in order:

    Monday 10:00
    Monday 15:00
    Friday 10:00
    Friday 15:00
    Monday 10:00

Consider the following timer:

    mon,10:00,,fri,15:00

The next 3 events in this case are:

    Monday 10am
    Friday 15pm
    Monday 10am

All of these examples work on a weekly basis, but certain events are better scheduled on a monthly basis. To support that, weekdays may be suffixed by `wspec` entry that defines the week number inside the month. As an example, the following timer defines two events every month, on the first and third Mondays at 15:00:

    mon1,mon3,15:00

As a special case, the 5th week is considered the last one to hold the given day, so that specifying an event on the last Friday of the month, for instance, is done simply as:

    fri5

In addition to specifying precise weekdays, an interval may be used to define a larger span:

    mon-fri,15:00

This represents an event per day at 15:00, Monday through Friday, every week.

The same interval syntax also works to define time spans, but the meaning is slightly different. For instance, consider this time span:

    mon,14:00-16:00

It defines an event every Monday that will take place at the earliest chance between 14:100 and 16:00. 

Weekday spans define an event **every day** within the span. In contrast, time spans define a **single event** inside the defined span.

That latter aspect may be changed via an explicit divisor, which may be specified as a `count`. For instance, consider this time span:

    8:00-16:00/2

This represents two events every day, one in the morning between 8:00 and 12:00, and another one between 12:00 and 16:00.

While the following represents an hourly event, every day of the week:

    0:00-24:00/24

All of the time spans defined so far work similarly in the sense that the start of the span defines the earliest chance in which the event may start, and the end of the span defines the latest chance for the event to have started. For various reasons, though, it’s often useful to introduce some level of randomization inside the time span so that events won’t all start at exactly the same time. This may be achieved by replacing the time span dash character (`-`) by a tilde (`~`). Consider the following time span:

    0:00~24:00/4

It represents 4 events that will take place at a random time inside time windows of 6 hours each.

Week spans that need to start or end during a specific week in the month can be defined by appending the week number to either the start or end of a week span. Consider the following schedules:

    mon1-fri
    mon-fri1

The first example describes a week span that starts on the first Monday of the month and ends on the following Friday, while the second example defines a week span that starts on the Monday _before_ the first Friday of the month, which is when the span ends. 

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

In the above context, `mon1-fri` corresponds describes a span from 5th of August to the 9th of August, while `mon-fri1` covers 29th of July until 2nd of August.

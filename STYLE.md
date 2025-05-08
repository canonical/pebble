# Style guide

**Table of contents:**

* [Code decisions](#code-decisions)
* [Docs and docstrings](#docs-and-docstrings)

## Code decisions

This is the Go style guide we use for the Pebble project. It's also the style we're converging on for other projects maintained by the Charm Tech team.

New code should follow these guidelines, unless there's a good reason not to. Sometimes existing code doesn't follow these, but we're happy for it to be updated to do so (either all at once, or as you change nearby code).

Of course, this is just a start! We add to this list as things come up in code review; this list reflects our team decisions.


### Naming conventions

#### Use `CamelCase` or `camelCase`

The convention in Go is to use MixedCaps or mixedCaps rather than underscores to write multiword names.

Further more, abbreviations are always written with the same case, which means `HTTPPort`, not `HttpPort`.

#### Don't use `ALL_CAPS`

When it's not an environment variable, do not use `ALL_CAPS` because it looks like one.

#### Short names can be used in context

Avoid long names with redundant information which can be inferred from the context. Be concise - a long name can almost always be shortened. 

Example:

- Avoid: `basicAuthUsername, basicAuthPassword, _ := r.BasicAuth()`
- Prefer: `username, password, _ := r.BasicAuth()`

Because in the context, it can be inferred the username and password are for basic auth.

#### Be consistent

If `func foo()` returns a `code`, then `returnCode := foo()` is more accurate than `returnVal := foo()` - the result variable naming should be consistent with the function called.

If existing code uses `json:"error,omitempty"` instead of `json:"err,omitempty"`, stick to the existing convention.

If the rest of the API uses `verbNoun` then unless there is a very good reason not to, the next function should be of the form `verbNoun`.

### Adding code in existing functions and structs

Think stratigically about _where to add the new code._ Study existing code and try to discover a logic or pattern.

Order:

- A special order: For example, if you have a validation function that validates user inputs against a couple of rules and the code uses if/switch and puts them in alphabetical order, when adding new code, you should probably follow that existing pattern.
- Anywhere? This could work in certain cases. For the same example, if existing validation rules are organized in no particular order and now you are adding a new rule, it's probably ok to add it anywhere in the validation function. At the beginning, at the end, or even in the middle.
- Some logical order: For the same example, if the new rule you add is a naming convention (e.g., it must match a certain pattern), then it's almost certainly correct to put it at the very beginning: If the name doesn't even follow the convention, there's no need to check other rules. Lazy thinking would add this new rule anywhere, because it's just another rule; strategic thinking would try to discover the logic and find the best place.

Ownership:

- When adding a new field/flag to an existing struct, think about if the field really _belongs_ to the thing where you are adding it. Are you adding it there just because it's simple to do so, or does it truly belong to it? If not, should a new struct be created specifically for it? Understand existing code before working on it.

An example of order:

Avoid:

```go
MkdirOptions{
	MakeParents: true,
	ExistOK:     true,
	Chown:       true,
	Chmod:       true,
	UserID:      sys.UserID(*uid),
	GroupID:     sys.GroupID(*gid),
}
```

Prefer:

```go
MkdirOptions{
	MakeParents: true,
	ExistOK:     true,
	Chmod:       true,
	Chown:       true,
	UserID:      sys.UserID(*uid),
	GroupID:     sys.GroupID(*gid),
}
```

(Pay attention to the place of `Chown` because it relates more to `UserID` and `GroupID`.)

### Refactoring

Compare the logic before/after. There should be no behavior changes, because it's a "refactor", so make sure that the behavior is 100% the same.

### Code style

#### Functions

##### Arguments

Use Go style:

- Avoid `func foo(a string, b string, c string)`
- Prefer `func foo(a, b, c string)`

##### Named arguments

It's clearer to have named arguments when you're returning multiple things of the same type, and it's not clear which is which. Example:

- Avoid: `func foo() (<-chan servicelog.Entry, <-chan servicelog.Entry) {}`
- Prefer: `func foo() (stdoutCh <-chan servicelog.Entry, stderrCh <-chan servicelog.Entry) {}`

##### Short variable declarations

Inside a function, use the more go-idiomatic `:=` short assignment for var declaration with implicit type.

- Prefer: `a := 0`, even if you must explicitly assign a zero value. This will help signify that there are code-paths which read this value before it is assigned to.
- Avoid: `var a = 0`

On the other hand, if the zero value you'd write with the first way would never be read, use `var foo type`. For example:

```go
	var numberName string
	if i == 1 {
		numberName = "one"
	} else {
		numberName = "not one"
	}
```

##### Don't repeat yourself

Example:

Avoid:

```go
// Create a single directory and perform chmod/chown operations according to options.
func mkdir(path string, perm os.FileMode, options *MkdirOptions) error {
	// multiple options != nil
	if options != nil && options.Chown {
		// ...
	}

	// multiple options != nil
	if options != nil && options.Chmod {
		// ...
	}

	// ...
}
```

Prefer:

```go
func Mkdir(path string, perm os.FileMode, options *MkdirOptions) error {
	// avoid multiple options != nil
	if options == nil {
		options = &MkdirOptions{}
	}
	
	if options.Chown {
		// ...
	}

	if options.Chmod {
		// ...
	}

	// ...
}

```

##### Avoid very small functions

For example, a one-liner unexported function in the same package probably isn't necessary.

Another example:

```go
func (rb *RingBuffer) Positions() (start RingPos, end RingPos) {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	return rb.positions()
}

func (rb *RingBuffer) positions() (start RingPos, end RingPos) {
	return rb.readIndex, rb.writeIndex
}
```

The two functions don't provide much value because we can simply use 

```go
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()

	start := rb.readIndex
	stop := rb.writeIndex
```

Another example: Use `strings.Contains` instead of creating a function `containsSubstring`.

Exception: If a very short function is used repeatedly in different places and increases readability, maybe we can keep it.

##### Cuddled braces

Avoid:

```go
	err = osutil.Mkdir(
		filepath.Dir(filename),
		0700,
		&osutil.MkdirOptions{
			ExistOK: true,
			Chmod:   true,
			UserID:  uid,
			GroupID: gid,
		},
	)
```

Prefer:

```go
	err = osutil.Mkdir(filepath.Dir(filename), 0700, &osutil.MkdirOptions{
		ExistOK: true,
		// ...
	})
```

##### Exported or unexported

`lowerCaseFunction` if it doesn't need to be exported (for example, helper functions). Think carefully about if it really needs to be exported.

##### Guard `nil` values

Guard with `if foo != nil { }` as early as possible, before the first possible access.

##### Defer

If you are adding a piece of code that could potentially return prematurely in a function where there are deferred calls, you'd probably want to add your new code _after_ the deferred calls to make sure the defer is always called.

##### Chaining function calls

Split into multiple lines for long chaining calls so it's easier to see what's being called. Example:

- Avoid: `err := c.d.overlord.CheckManager().RunCheck(r.Context(), check)`
- Prefer:

```go
	checkMgr := c.d.overlord.CheckManager()
	checks, err := checkMgr.Checks()
	if err != nil {
		return InternalError("%v", err)
	}
```

#### Struct

##### Grouping

Grouping related fields together in a struct.

For example:

Avoid:

```go
type MkdirOptions struct {
	MakeParents bool

	ExistOK bool

	Chmod bool

	Chown bool

	UserID sys.UserID
	
	GroupID sys.GroupID
}
```

Prefer:

```go
type MkdirOptions struct {
	MakeParents bool

	ExistOK bool

	Chmod bool

	Chown bool
	UserID sys.UserID
	GroupID sys.GroupID
}
```

Because all these three fields are related to ownership.

##### Pointer vs value for struct field

Whether to use a pointer or a direct value in a struct depends on several factors:

Use pointer when:

- The embedded struct is large: using a pointer avoids copying the entire struct when passing it around.
- Optional field: If the field can be nil/null (truly optional), a pointer makes this explicit.
- Mutable shared state: If you need to modify the original data from different places.
- JSON null values: If you need to distinguish between a zero-valued struct and an absent/missing value in JSON (pointer can be nil).

Use direct value when:

- Small struct
- Required field: If the field should never be nil in a valid struct.
- Value semantics: If you want each struct to have its own copy of the data.
- Simplicity: Avoiding nil checks and pointer dereferencing can make code simpler.

Common Practice: Use pointers for struct fields when:

- The struct is reasonably large
- The field is truly optional
- You need to share the same instance between different objects

##### Trailing comma

Trailing comma after the last field, the closing brace is on its own line.

Avoid:

```go
	check = &checkData{
		name:    name,
		refresh: make(chan struct{}),
		result:  make(chan error)}
```

Prefer:

```go
	check = &checkData{
		name:    name,
		refresh: make(chan struct{}),
		result:  make(chan error),
	}
```

#### Synchronization

##### Locks

Suppose that func A calls B, where B locks/unlocks a lock, and then A also locks/unlocks the same lock. This is messy and inefficient. Put everything lock-related in A.

Example:

Avoid:

```go
func (rb *RingBuffer) reverseLinePosition(n int) RingPos {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	// ...
}

func (rb *RingBuffer) HeadIterator(lines int) Iterator {
	firstLine := rb.reverseLinePosition(lines)
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	// ...
}
```

Prefer:

```go
func (rb *RingBuffer) reverseLinePosition(n int) RingPos {
	// ...
	// no lock
}

func (rb *RingBuffer) HeadIterator(lines int) Iterator {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	firstLine := rb.reverseLinePosition(lines)
	// ...
}
```

##### Channels

Cancellation channels should be unbuffered channels that are closed.

```go
	stopStdout := make(chan struct{})
	// ...
	close(stopStdout)
```

##### Channel over sleep

Prefer:

```go
	timeoutCh := time.After(timeout)
	for {
		select {
		case foo, ok := <-fooCh: ...
		case <-timeoutCh: ...
		}
	}
```

Instead of sleep.

##### Switch default

For defensive programming, probably best to add a default case.

#### Strings

##### String concatenation or `fmt.Sprintf`

It's simpler and more efficient to avoid fmt.Sprintf for simple concatenation, like `"FOO="+foo` over `fmt.Sprintf("FOO=%s", foo)`.

_Although this is debatable because, from the perspective of readability, one can argue that `fmt.Sprintf` wins. When in doubt, respect existing code conventions._

##### Multiline strings

It's easier to use `[1:]` for readability. Example:

Avoid:

```go
	expected := `This
is a 
multiline
string.
`
```

Prefer:

```go
	expected := `
This
is a 
multiline
string.
`[1:]
```

However, if it's YAML, YAML doesn't care about an empty line in the beginning, so it's OK to not have `[1:]`:

```go
	someYAML := `
key: value
foo: bar
`
```

#### Regex

We shouldn't re-compile the regex (a relatively expensive operation) every time we call a function. The regex compilation should be done at the top level so `MustCompile` is run once on package init.

Avoid:

```go
func foo() {
	var nameRegexp = regexp.MustCompile(`^[a-z0-9]+$`)
	// ...
}

```

Prefer:

```go
var nameRegexp = regexp.MustCompile(`^[a-z0-9]+$`)

func foo(name string) {
	if !nameRegexp.MatchString(name) {
		// ...
	}
	// ...
}
```

#### Permissions

Prefer `0o755` over `0755` format to make it super-clear it's octal, unless it's already `0755` in existing code.

#### `iota`

The `iota` identifier is used in const declarations to simplify definitions of incrementing numbers. The `iota` keyword represents successive integer constants 0, 1, 2, ... It resets to 0 whenever the word `const` appears in the source code, and increments after each `const` specification. To avoid getting the zero value by accident, we can start a list of constants at 1 instead of 0 from `iota + 1`.

### Error handling

#### Be specific

When creating an error message, think from the user's perspective, and see what specific messages would help them the most.

For example, if the user input layer label is `pebble-test` but pebble-* is reserved:

- `fmt.Sprintf("cannot use reserved layer label %q", layer.Label)`
- ``cannot use reserved label prefix "pebble-"``

Is it because the `pebble-test` label is reserved? Or is it because the `pebble-` prefix is reserved? The latter is true, hence prefer the second error message, and avoid the first. Be specific.

For another example:

- Avoid `fmt.Println("Setup failed with error:", err)` (Ambiguous)
- Prefer `fmt.Println("Cannot build pebble binary:", err)` (Specific - we know why it fails)

#### Be consistent

The form of the phrases used in error messages should be consistent with other code in the same function or even the same module. For example, if existing code uses:

```go
	if !osutil.IsDir(o.pebbleDir) {
		return nil, fmt.Errorf("directory %q does not exist", o.pebbleDir)
	}
```

When adding a new error for no write permissions:

- Avoid: `fmt.Errorf("no write permission in directory %q", o.pebbleDir)`
- Prefer: `fmt.Errorf("directory %q not writeable, o.pebbleDir)` (Follow existing convention)

#### Use `errors.Is`

Use `errors.Is()` to check error types.

```go
	if errors.Is(err, fs.ErrNotExist) {
		// ...
	}
```

#### Use custom error types

Don't check the error string, which is fragile; use a custom error type.

Examples:

```go
type detailsError struct {
	error
	details string
}

message := err.Error()

var detailsErr *detailsError

if errors.As(err, &detailsErr) && detailsErr.Details() != "" {
	message += "; " + detailsErr.Details()
}
```

In general, a custom error type should be considered as a marker indicating that an error is somehow "recoverable".

Use a custom type if the consumer of your API can be expected to perform some special action in response to an error. If necessary, add public fields to expose data which allows a user to make good decisions about what just went wrong.

Do not use a custom error type if, as is more likely, you are writing an error which the user shouldn't handle specially, but which isn't catastrophic enough to take down the entire system (remember panicking can take down everything else too). In this case, stick to a general-purpose error by using `errors.New` or `fmt.Errorf`.

#### Variables

Avoid hard-coded values in errors. Example:

- Avoid: `fmt.Errorf("stopped before the 1 second okay delay")`
- Prefer: `fmt.Errorf("stopped before the %s okay delay", okayDelay)`

#### Wrap error with more context

A low-level error starts to become meaningless as the circumstances which caused it get lost as it passes up the stack. Instead of returning an error, which might not be a whole lot of information, wrap it with more context. For example:

Avoid:

```go
	logs, err := cmd.taskLogs(info.ChangeID)
	if err != nil {
		return err
	}
```

Prefer: `fmt.Errorf("cannot get task logs for change %s: %w", info.ChangeID, err)`

Using `%w` implies that the wrapped error may later be inspected in order to perform some specific action in response. Use this to allow for error recovery. Using `%v` implies that the wrapped error is unrecoverable. Given that the only way to extract the underlying error would be to use very fragile string matching, this method very clearly discourages such attempts.

### Tests

#### Test naming

Put tremendous effort into test names.

Do not just use some casual name that comes to you off the top of your head. Use meaningful, precise names that follow the convention of existing code.

For the same example as mentioned in the error messages section, suppose that we have a validation function that validates the user input layer label to catch reserved prefixes:

- Follow Convention: If all tests in the same file follow the "Test(Do)Something(SomeFeature)" convention, for example `TestParseCommand` or `TestMergeServiceContextOverrides`, follow the same convention when adding a new test. `TestPebbleLabelPrefixReserved` probably fits better in the context than `TestCannotUseReservedPrefixInLayers`.
- Be Precise: Are we testing parsing the layer (then see if the label is valid) or are we testing the labels themselves? The latter is more true, hence `TestParseLayer` is not as accurate as `TestLabel`. For another example, don't use `TestNormal` which is too generic; `TestStartupEnabledServices` is a whole lot better as the name of a test.

Following the rules above, the best name probably is `TestLabelReservedPrefix` or `TestLabelReservedPrefix`. Although short names can be used in context, test names are places where longer but precise names are better than short ones.

#### Variable names

Use `foo`, `bar`, `baz`, `qux`, `quux`, and other metasyntactic variables and placeholder names in tests.

However, check existing code and be consistent: If existing tests use "alice" and "bob" or meaningful variable names like `pebble-service-name`, follow the convention. Conversely, do not use very specific names that actually mean something when it's just a generic name.

#### Copy-paste

When adding unit tests, it's common to copy-paste an existing test which is similar and modify that because it's quicker. It's OK to copy-paste, but examine the naming, the logic, remove unnecessary things carefully. Treat it as if you are writing a new test.

It's also OK to use a few lines of duplicated code if it makes the test clearer, instead of creating helper functions. See [Advanced Testing with Go - Mitchell Hashimoto](https://www.youtube.com/watch?v=8hQG7QlcLBk).

#### Setup/teardown

It is sometimes necessary for a test to do extra setup or teardown. To support these and other cases, if a test file contains a function:

`func TestMain(m *testing.M)`

Then the generated test will call `TestMain(m)` instead of running the tests directly.

TestMain runs in the main goroutine and can do whatever setup and teardown is necessary around a call to `m.Run`.

A simple implementation of TestMain is:

```go
func TestMain(m *testing.M) {
	// global setup here
	code := m.Run() 
	// global tear down here
	os.Exit(code)
}
```

TestMain is a low-level primitive and should not be necessary for casual testing needs, where ordinary test functions suffice.

#### ENV vars

`Setenv` calls `os.Setenv(key, value)` and uses Cleanup to restore the environment variable to its original value after the test. So, instead of doing:

```go
	os.Setenv("FOO", "1")
	defer os.Setenv("FOO", "")
```

We can simply: `t.Setenv("FOO", "1")`.

#### Sending signals

In the context of process termination, SIGTERM (signal 15) is a polite request for a process to shut down gracefully, allowing it to perform cleanup tasks, while SIGKILL (signal 9) forcefully terminates the process immediately, without any cleanup.

In tests, try to use SIGTERM instead of SIGKILL for a graceful termination.

#### Avoid cached results with `-count=1`

Test outputs are cached to speed up tests. If the code doesn't change, the test output shouldn't change either. Of course, this is not necessarily true in practice - tests may read info from external sources or may use time and random related data which could change from run to run.

When you request multiple test runs using the `-count` flag, the intention is to run the tests multiple times. There's no point running them just once and showing the same result n-1 times. So `-count` triggers omitting the cached results.

Setting `-count=1` will cause the tests to run once, omitting previously cached outputs. (The default value of `count` is 1, but you need to explicitly set the value to 1 to change the caching behavior.)

#### `t.Fatalf` or `t.Errorf`?

If a test should not continue at a certain point, use `t.Fatalf` instead of `t.Errorf`. Do not use `t.Errorf` without thinking about it everywhere. There is a difference between them.

#### Ports

When testing a port, use a high port that's not likely to be used. A port above 60000 would be better than 4000 or 8080.

#### Comments in tests

All complex tests should have a verbose comment describing what they are testing.

#### Philosophies

##### Avoid defaults

Try not to have defaults for tests, as they often get in the way of additional tests.

##### Check behaviour, not logs

Do not bother checking the logs in the tests, because stable log formatting isn't part of the contract. Check the expected behaviour or output.

##### Time-dependent tests

When a test needs to wait (e.g., in a for loop or a sleep), make sure the time is long enough to ensure that the test passes even when the CPU is loaded. Also make sure the time is not excessively long, which would slow the tests drastically. Use a reasonable value, refer to existing tests, follow existing convention, and maybe run the test multiple times to get a reasonable value - do not simply set a value and leave it at that.

##### Test coverage

Do the tests cover all scenarios?

Do all newly added functions have tests?

### Comments

#### Be precise

Think about choice of words, especially verbs. Are errors "returned" or "thrown"?

#### Helper/utility functions

Add comments for complex helper/utility functions to describe their use.

#### Don't write obvious comments

Either delete an obvious comment or make it more useful.

For example:

- Avoid: "MkdirOptions is a struct of options used for Mkdir()."
- Prefer: Either remove the comment or: "MkdirOptions holds the options for a call to Mkdir."

For another example:

```go
	// if path already exists
	if s, err := os.Stat(path); err == nil {
		// ...
	}
```

The comment probably isn't necessary since the following line is straightforward to understand.

#### Use `TODO`

Add a "TODO" in the comment when handling temporary workarounds so it's easier to grep for later.

#### Housekeeping rule

Be careful when the code you change has some comments/notes or even links to issues. Read them carefully. If your code change solves the issue, remove the link - keep the comment clean and true.

#### Spell-check and grammar-check comments

Do this.

#### Review newly added/updated comments before committing

Are they precise, are they specific, are they correct, both grammatically and literally?

## Docs and docstrings

### Tones

#### Avoid negative, be positive

State conditions positively as what should happen, rather than what shouldn't. Focus on desired behaviour, and frame instructions and conditions around the expected or successful outcome, not failure cases.

Example:

- Avoid: "If the command doesn’t exit within 1 second, the start is considered successful." (Netagive)
- Prefer: "If the command stays running for the 1-second window, the start is considered successful." (Positive)

Exceptions: Only use negative phrasing (e.g., "if X does not happen") when:

- The failure case is the primary concern (e.g., error handling).
- The positive phrasing is awkward or less clear.

#### Avoid passive, be active

Use the active voice as much as possible.

We should only use the passive voice when we don't know or care about who performed the action.

Example:

- Avoid: "A minimum check is created using the default values"
- Prefer: "We create a minimum check, using the default values"

#### Avoid subjective, be objective

Instead of using words like "easy," "simple," or "just," which are very subjective and assume the reader's skill level, describe the action directly by stating what to do and focusing on concrete steps.

Examples:

- Avoid: "This can be done easily using ..."
- Prefer: "This can be done using ..."

- Avoid: "This can be easily configured by ..."
- Prefer: "We can configure this using ..."

- Avoid: "Simply run the command ..."
- Prefer: "Run the command ..."

### Structure

#### Be short

For example, a very long introduction part of a how-to guide or a tutorial probably isn't necessary; it's good if we are writing a blog post or a book, but not so much for a tutorial/how-to guide type of doc.

#### Be concise and reduce repetition

Trim repetitive titles: Avoid repeating the name if the surrounding content already provides context.

For example, in the doc "how to use pebble", the section names can simply be "API", "CLI commands", "Features", etc., instead of "Pebble API", "Pebble CLI commands", "Pebble features".

#### Order: alphanumeric or logical

We can either use an alphanumeric order, or a logical order, depending on the context.

Alphanumeric order is better in the case of a reference doc or a list (e.g., an Enum).

Example (Enum):

- Prefer: `["autostart", "replan", "restart", "start", "stop"]`
- Avoid: `["start", "stop", "restart", "replan", "autostart"]`

Example (A list of commands):

Avoid:

- `pebble ls`
- `pebble mkdir`
- `pebble rm`
- `pebble push`
- `pebble pull`

Prefer:

- `pebble ls`
- `pebble mkdir`
- `pebble pull`
- `pebble push`
- `pebble rm`

Exceptions:

- If there's a strong logical progression (e.g., "low", "medium", "high"; or a list of things that you should read in that particular order).
- If there is already an exception in the current documentation, follow existing conventions.

### Content

#### Spell out abbreviations

Abbreviations and acronyms in docstrings should usually be spelled out:

- "for example" rather than "e.g."
- "that is" rather than "i.e."
- "and so on" rather than "etc"
- "unit testing" rather than UT, and so on

However, it's okay to use acronyms that are very well known in our domain, like HTTP or JSON or RPC.

#### Use sentence case in headings

`## Use sentence case for headings`, instead of `## Use Title Case for Headings`.

#### Be consistent

Be consistent in every possible way as much as possible, and stay context-consistent.

Choice of words: For example, if the whole document uses "mandatory", you probably shouldn't use "required" in a newly added paragraph. For another example, if the whole doc uses "list foo" when adding new content, don't use "get a list of bar".

Headings/lists: For example, if a doc is called "How to use Pebble", with sections "API", "CLI commands", "Health checks", and "Specification", use the heading "Environment variables" for a new section about environment variables, instead of the heading "Pebble environment variables".

#### Be precise

Be precise in names and verbs. Use precise verbs to describe the behaviour. For example, the appropriate description of `/v1/services` is "list services", while "get a service" is probably a better fit for `/v1/services/{name}`.

Split distinct ideas: Use separate sentences/clauses and avoid cramming. Don’t merge unrelated details (e.g., parameter format + default behaviour) into a single phrase.

Example:

- Avoid: "The names of the services to get, a comma-separated string. If empty, get all services."
- Prefer: "The names of the services to get. Specify multiple times for multiple values. If omitted, the method returns all services." (In three short precise sentences we've covered what the parameter does, usage details, and behavioral details.)

Example:

- Avoid: "For reference information about the API, see [API and clients] and [API]." (What's the difference of these two?)
- Prefer: "For an explanation of API access levels, see [API and clients]. For the full API reference, see [API reference]." (Clear)

#### Don't over-promote

Avoid overstatement: Only use adjectives like "comprehensive", "powerful", or "robust" when the feature truly meets the description.

Example:

- Avoid: "Pebble provides a comprehensive health check feature"
- Prefer: "Pebble provides a health check feature"

Let features speak for themselves, describe actual capabilities and use measurable terms when possible (e.g., "supports 3 types of checks" instead of "versatile checking")

Future-proofing:

- Avoid absolute terms unless permanently true
- Qualify with "currently" when describing evolving features

#### Avoid implementation-specific terminology

Use generic descriptions instead of what's specific to the code. Focus on behaviour, not internal implementation: Describe what it does (e.g., "returns an error message") rather than how a particular implementation works.

Example:

- Avoid: "`Change.Err` will be non-empty if the change had an error." (Code-specific)
- Prefer: "The `err` field in the response will contain an error message if the operation failed." (General)

Exception: If documenting a client library (e.g., Go/Python SDKs), implementation details are appropriate.

#### Articles

"a" or "the", generic or specific, choose carefully. When describing generic parameters or behavior:

- Avoid: "The format of the duration string is a sequence of decimal numbers"
- Prefer: "The format of a duration string is a sequence of decimal numbers." (Describing a generic parameter, not a specific one.)

- Avoid: "Restart the service when the health check fails."
- Prefer: "Restart a service when the health check fails." (When describing a generic behaviour, no specific service is implied.)


### Code blocks

- Consistency: the styles of code blocks and terminal output samples should be the same, at least within the same document.
- Preferred style: Use `{code-block}` when showing files. Use `{terminal}` (`.rst` style) when showing commands or terminal output. Avoid using `{code-block}` for commands or terminal output, unless it's consistent with the existing content in the same document.
- Highlighting: Use `:emphasize-lines: 8-10` with `{code-block}` for highlighting lines in files when necessary. Don't add an inconsistent `{code-block}` so that you can highlight lines in commands or terminal output. Instead, use `{terminal}` along with helpful words and comments.

### YAML

- Types: Handle strings and integers carefully. For example, if the field "change" is a string in the code, use `change: "1"` in the YAML example, not `change: 1`.
- Use quotes for strings: This is especially important if a string contains special characters or starts with a number.
- Indentation: Always use spaces and be consistent with the number of spaces throughout the same file (usually, this should be two or four).
- Use comments: Comments will help you and others understand what that data is used for.

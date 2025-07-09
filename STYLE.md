# Pebble Go style guide

This is the Go style guide we use for the Pebble project. It's also the style we're converging on for other Go projects maintained by the Charm Tech team.

New code should follow these guidelines, unless there's a good reason not to. Sometimes existing code doesn't follow these, but we're happy for it to be updated to do so (either all at once, or as you change nearby code).

Of course, this is just a start! We add to this list as things come up in code review; this list reflects our team decisions.

For our documentation style guide, see [STYLE.md in the canonical/operator repo](https://github.com/canonical/operator/blob/main/STYLE.md#docs-and-docstrings).

## Naming conventions

### Use `CamelCase` or `camelCase`

The convention in Go is to use `MixedCaps` for exported names and `mixedCaps` for local variables and non-exported names. Don't use underscores to separate words in multi-word names.

Abbreviations should always be written with the letters in the same case, for example `HTTPPort` or `httpPort`, not `HttpPort`.

### Don't use `ALL_CAPS`

Use `MyConst` for constants (referenced as `mypkg.MyConst`). Only use `ALL_CAPS` for environment variable names.

### Short names can be used in context

Be concise: avoid long names with redundant information which can be inferred from the context.

- Avoid: `basicAuthUsername, basicAuthPassword, _ := r.BasicAuth()`
- Prefer: `username, password, _ := r.BasicAuth()`

In context, it's obvious that the username and password are for basic auth.

### Be consistent

If `foo()` returns a "code", then `code := foo()` is more accurate and more concise than `returnVal := foo()` -- the result variable should be named consistently with the function called.

If the rest of the API uses `verbNoun` then unless there is a very good reason not to, the next function should be of the form `verbNoun`. For example, if the API has `createUser()`, `updateUser()`, `deleteUser()`, then adding `getUser()` fits perfectly, but if you add `userFetch()` instead of `fetchUser()`, it breaks the consistent pattern, making the API harder to learn and use.

### Adding code to existing functions and structs

Think stratigically about _where to add the new code._ Study existing code and try to discover a logic or pattern.

Order:

- Alphabetical order: For example, if you're in a function with a switch or series of ifs, and the existing code puts the cases in alphabetical order, follow the existing pattern.
- Another logical order: For example, by frequency of use. If you're adding a new case to a switch statement, it makes sense to put the most frequently used case at the beginning instead of following alphabetical order.
- Ownership: when adding a new field to a struct, ask if the field really _belongs_ to the struct you're adding it to? Add the new field to the code that "owns" it, even if it's simpler to add it to another struct.

As an example, consider the placement of the Chown field below. It relates to UserID and GroupID, so it should go directly above them:

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

## Code style

### Merge arguments of the same type

- Avoid `func foo(a string, b string, c string)`
- Prefer `func foo(a, b, c string)`

### Named return values

It's usually clearer to use named arguments when you're returning multiple values of the same type, for example:

- Avoid: `func run() (<-chan servicelog.Entry, <-chan servicelog.Entry) {}`
- Prefer: `func run() (stdoutCh, stderrCh <-chan servicelog.Entry) {}`

### Short variable declarations

Where possible, use the more idiomatic `:=` short form variable declaration with an implicit type.

- Prefer: `a := 0`, even if you must explicitly assign a zero value. This will help signify that there are code-paths which read this value before it is assigned to.
- Avoid: `var a = 0`

On the other hand, if the zero value would never be read, use `var foo type`. For example:

```go
var name string
if i == 1 {
	name = "one"
} else {
	name = "not one"
}
```

That said, in cases like this is sometimes simpler to initialise to the `else` value instead:

```go
name := "not one"
if i == 1 {
    name = "one"
}
```

### Don't repeat yourself (DRY)

Avoid repeating code where possible.

For example, imagine you had to greet many people, sometimes formally, sometimes informally. Without DRY, you'd write similar `fmt.Println` statements over and over. If you later wanted to change the greeting, you'd have to change every single line where you used it. That's error-prone and time-consuming. Prefer the DRY Solution:

```go
// Greet prints a greeting message.  Avoid repeating the greeting logic!
func Greet(name string, formal bool) string {
	greeting := "Hello, "
	if formal {
		greeting = "Greetings, esteemed "
	}
	return greeting + name + "!"
}

func main() {
	fmt.Println(Greet("Alice", false))   // Hello, Alice!
	fmt.Println(Greet("Bob", true))      // Greetings, esteemed Bob!
	fmt.Println(Greet("Charlie", false)) // Hello, Charlie!
}
```

This reduces redundancy, is less error-prone, improves readability of the main function (instead of a bunch of prints) and increases reusability.

### Avoid very small functions

In many cases, it's not worth creating a function or method for one-liners that aren't exported. For example, below you could simply write `positions` inline:

Avoid:

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

Prefer:

```go
func (rb *RingBuffer) Positions() (start RingPos, end RingPos) {
	rb.rwlock.RLock()
	defer rb.rwlock.RUnlock()
	return rb.readIndex, rb.writeIndex
}
```

### Cuddled braces

Avoid:

```go
err = osutil.Mkdir(
	filepath.Dir(filename),
	0o700,
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
err = osutil.Mkdir(filepath.Dir(filename), 0o700, &osutil.MkdirOptions{
	ExistOK: true,
	Chmod:   true,
	UserID:  uid,
	GroupID: gid,
})
```

### Chaining function calls

Split long chained calls into multiple lines so it's easier to see what's being called. For example:

- Avoid:

```go
err := c.d.overlord.CheckManager().RunCheck(r.Context(), check)
```

- Prefer:

```go
checkMgr := c.d.overlord.CheckManager()
err := checkMgr.RunCheck(r.Context(), check)
if err != nil {
	return InternalError("%v", err)
}
```

### Grouping struct fields

Group related fields together in a struct. For example:

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

	Chown   bool
	UserID  sys.UserID
	GroupID sys.GroupID
}
```

### Pointer vs value for struct field

Whether to use a pointer or a direct value in a struct depends on several factors:

Use a pointer when:

- The embedded struct is large: using a pointer avoids copying the entire struct when passing it around.
- It's an optional field and the zero value is a valid value.
- Mutable shared state: if you need to modify the original data from different places.
- JSON null values: if you need to distinguish between a zero-valued struct and a missing value in JSON (nil pointer).

Use a value when:

- The included struct is small.
- It's a required field: if the field should never be nil in a valid struct.
- You need value semantics: if you want each struct to have its own copy of the data.

See more: [Receiver Type](https://go.dev/wiki/CodeReviewComments#receiver-type).

### Trailing commas

Add a trailing comma after the last field in a struct or line in an argument list, with the closing brace on its own line.

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

### Locks

Suppose that func A calls helper B, where B locks and unlocks a mutex, and A also locks and unlocks the same mutex. This is messy and inefficient. Instead, put everything lock-related in A. For example:

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

### Cancellation channels

Cancellation channels should be unbuffered channels that are closed. For example:

```go
	stopStdout := make(chan struct{})
	// ...
	close(stopStdout)
```

### Prefer `time.After` over `time.Sleep`

Using `time.Sleep` is not cancelable, so in cases where you need a cancelable sleep, use `time.After` with a `select`. For example:

```go
select {
case <-time.After(duration):
	return nil // Slept the full duration
case <-ctx.Done():
	return ctx.Err() // Canceled!
}
```

### String concatenation or `fmt.Sprintf`

It's simpler and more efficient to avoid `fmt.Sprintf` for simple concatenation.

- Avoid: `fmt.Sprintf("FOO=%s", foo)`
- Prefer: `"FOO="+foo`

However, for more complex cases, fmt.Sprintf is usually clearer:

- Avoid: `name + " is " + strconv.Itoa(age) + " years old"`
- Prefer: `fmt.Sprintf("%s is %d years old", name, age)`

### Multiline strings

It's sometimes useful, especially in tests, to add `[1:]` to multiline strings for readability. This allows the first line to start at column 1.

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

However, if it's JSON or YAML, an empty line at the beginning doesn't matter, so you can avoid the `[1:]`:

```go
someYAML := `
key: value
foo: bar
`
```

### Regex

Don't re-compile a `regexp.Regexp` every time you call a function (it's a relatively expensive operation). Instead, use `MustCompile` at the package level, so that compilation is done once on package init.

Avoid:

```go
func foo(name string) {
	nameRegexp := regexp.MustCompile(`^[a-z0-9]+$`)
	if !nameRegexp.MatchString(name) {
		// ...
	}
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

### Octal number literals

For new code, prefer the `0o755` format over `0755`, to make it very clear the number is octal.

## Error handling

### Start error messages with "cannot"

Where possible, start error messages with "cannot X" for consistency.

- Avoid: `fmt.Errorf("failed to open file: %w", err)`
- Prefer: `fmt.Errorf("cannot open file: %w", err)`

### Be specific

When creating an error message, think from the user's perspective, and see what specific messages would help them the most.

For example, if the user input layer label is `pebble-test` but pebble-* is reserved, be specific in your error message:

- Avoid: `fmt.Errorf("cannot use reserved layer label %q", layer.Label)`
- Prefer: `errors.New("cannot use reserved label prefix "pebble-")`

For another example:

- Avoid: `fmt.Println("Setup failed with error:", err)`
- Prefer: `fmt.Println("Cannot build pebble binary:", err)`

### Be consistent

The form of the phrases used in error messages should be consistent with other code in the same function or even the same module. For example, if existing code uses:

```go
	if !osutil.IsDir(o.pebbleDir) {
		return nil, fmt.Errorf("directory %q does not exist", o.pebbleDir)
	}
```

When adding a new error for no write permissions:

- Avoid: `fmt.Errorf("no write permission in directory %q", o.pebbleDir)`
- Prefer: `fmt.Errorf("directory %q not writeable, o.pebbleDir)` (Follow existing convention)

### Use `errors.Is`

Use `errors.Is()` to check error types, for example:

```go
if errors.Is(err, fs.ErrNotExist) {
	// ...
}
```

### Use custom error types

Don't check the error string, which is fragile; use a custom error type.

Avoid:

```go
err := doSomething()
if err != nil && strings.Contains(err.Error(), "file not found") {
    // Handle "file not found" case
}
```

Prefer:

```go
// Define a custom error type
type NotFoundError struct {
    Path string
}

func (e *NotFoundError) Error() string {
    return fmt.Sprintf("file not found: %s", e.Path)
}

// Check using errors.Is/As
err := doSomething()
var notFoundErr *NotFoundError
if errors.As(err, &notFoundErr) {
    // Handle "file not found" case
    fmt.Printf("Missing file at: %s", notFoundErr.Path)
}
```

In general, a custom error type should be considered as a marker indicating that an error is somehow "recoverable".

Do not use a custom error type if you're returning an error which the user shouldn't handle specially. In this case, stick to a general-purpose error by using `errors.New` or `fmt.Errorf`.

### Variables

Avoid hard-coded values in errors. Example:

- Avoid: `fmt.Errorf("stopped before the 1 second okay delay")`
- Prefer: `fmt.Errorf("stopped before the %s okay delay", okayDelay)`

### Wrap error with more context

A low-level error becomes less useful as it passes up the stack without context. Instead of returning the error directly, wrap it with more context.

Avoid:

```go
logs, err := cmd.taskLogs(info.ChangeID)
if err != nil {
	return err
}
```

Prefer:

```go
logs, err := cmd.taskLogs(info.ChangeID)
if err != nil {
	return fmt.Errorf("cannot get task logs for change %s: %w", info.ChangeID, err)
}
```

Using `%w` implies that the wrapped error may later be inspected in order to perform some specific action in response. Using `%v` implies that the wrapped error is unrecoverable. Given that the only way to extract the underlying error would be to use very fragile string matching, using `%v` clearly discourages such attempts.

## Tests

### Test naming

Put effort into test names. Use meaningful, precise names that follow the conventions of existing code.

- Follow Convention: If all tests in the same file follow the "Test(Something)(SomeFeature)" convention, for example `TestParseCommand` or `TestMergeServiceContextOverrides`, follow the same convention when adding a new test.
- Be Precise: Are we testing parsing the layer (to check if the label is valid) or are we testing the labels themselves? If the latter, `TestParseLayer` is not as accurate as `TestLabel`.

### Variable names

Use `foo`, `bar`, `baz`, `qux`, `quux`, and other metasyntactic variables and placeholder names in tests.

However, check existing code and be consistent: If existing tests use "alice" and "bob" or meaningful variable names like `pebble-service-name`, follow the convention. Conversely, do not use very specific names that actually mean something when it's just a generic name.

### Copy-paste

When adding unit tests, it's common to copy-paste an existing test which is similar and modify that. It's okay to copy-paste, but examine the naming and logic, and remove unnecessary things. Treat it as if you are writing a new test.

It's also okay to use a few lines of duplicated code if it makes the test clearer, instead of creating helper functions. See [Advanced Testing with Go by Mitchell Hashimoto](https://www.youtube.com/watch?v=8hQG7QlcLBk).

### Environment variables in tests

`Setenv` calls `os.Setenv(key, value)` and uses Cleanup to restore the environment variable to its original value after the test. So, instead of doing:

Avoid:

```go
func TestSomething(t *testing.T) {
    // Manually set and cleanup env var
    originalValue := os.Getenv("FOO")
    os.Setenv("FOO", "1")
    defer os.Setenv("FOO", originalValue)  // Risky - might not restore properly if test fails

    // Test code that uses FOO environment variable
}
```

Prefer:

```go
func TestSomething(t *testing.T) {
    // Let the testing package handle cleanup automatically
    t.Setenv("FOO", "1")  // Automatically restored after test

    // Test code that uses FOO environment variable
}
```

### `t.Fatalf` or `t.Errorf`?

If a test should not continue at a certain point, use `t.Fatalf` instead of `t.Errorf`. Do not use `t.Errorf` everywhere without thinking about it.

### Comments in tests

Complex tests should have a verbose comment describing what they are testing. Example:

```go
// TestCreateDirs tests that Pebble will create the Pebble directory on startup
// with the `--create-dirs` option.
func TestCreateDirs(t *testing.T) {
	tmpDir := t.TempDir()
	pebbleDir := filepath.Join(tmpDir, "pebble")
	_, stderrCh := pebbleDaemon(t, pebbleDir, "run", "--create-dirs")
	// ...
}
```

### Check behaviour, not logs

Do not bother checking the logs in the tests, because stable log formatting isn't part of the contract. Check the expected behaviour or output.

### Time-dependent tests

When a test needs to wait (in a `for` loop or a sleep), make sure the time is long enough to ensure that the test passes even when the CPU is loaded. Also make sure the time is not excessively long, which would slow the tests drastically. Use a reasonable value; refer to existing tests, follow existing convention, and maybe run the test multiple times to get a reasonable value.

## Comments

### Be precise

Think about choice of words, especially verbs. Are errors "returned" or "thrown"?

### Don't write obvious comments

Either delete an obvious comment or make it more useful.

For example:

- Avoid: "MkdirOptions is a struct of options used for Mkdir()."
- Prefer: Either remove the comment or: "MkdirOptions holds the options for a call to Mkdir."

For another example, the comment below isn't necessary since the following line is straightforward to understand:

```go
// If string has prefix "foo"
if strings.HasPrefix(s, "foo") {
    // ...
}
	if s, err := os.Stat(path); err == nil {
		// ...
	}
```

### Use `TODO`

Add a "TODO" in a comment when handling a temporary workaround, so it's easier to search for later. However, avoid merging TODO comments unless you plan to fix them in a follow-up PR.

### Housekeeping rule

Be careful when the code you change has some comments or links to issues. Read them carefully. If your code change solves the issue, remove the comment or link -- keep the comment clean and accurate.

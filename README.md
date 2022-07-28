# GoFind

GoFind finds text strings in files under a directory.

GoFind will search through the directory recursively in a new thread, any files it finds will be sent to a function in a separate thread to check the contents for the keywords in the `keywords` file. Files that contain one of the keywords will be listed in the `output` file. Any files which could not be accessed (due to permission errors, size limitations, etc.) will be listed in the `error` file. A log will be printed to the `log` file.

# Running GoFind

*GoFind ignores files in the `gofind` directory, please make sure to run this program into a directory named gofind.*

GoFind must be run with these arguments:
- `directory=` - Files to search through. If the directory has spaces, omit this argument. GoFind will ask for a directory after it is run.

These arguments are optional:
- `keywords=` - Keywords to use for the search, file must be a list of keywords separated by new lines.
- `regex=` - Regular Expressions to use for the search. The file must be a list of regexes separated by new lines.
- `output=` - Output .txt file, each found file will be on a new line with the path and keywords.
- `error=` - Lists files that could not be searched due to errors (permission denied, links to another file, etc.)
- `ignore=` - Skips a directory from being searched, only the directory name is needed, case insensitive (ex. `C:\Windows\System32` can be added as just `System32`)
- `ignoretypes=` - Skips file types from being searched, the file type should start with a period, case insensitive (ex. `.mov`)
- `newthread=` - Starts searching the files in a new thread rather than one thread for searching files. Faster but less reliable. Default is false
- `threadcount=` - Uses the number of threads given. Requires an integer number. Default is the number of cores on the system

Example commands:
- Windows: `gofind.exe directory=C:\`
- Linux: `gofind.elf directory=/`

# GoFind Output

## Output

The output file will be formatted like so:

`Path: RELATIVEPATHTOFILE | Keywords: FOUNDKEYWORD:LINENUMBER & FOUNDKEYWORD:REGEX:LINENUMBER & ...`

This regex will match the whole line (Path, Keywords):

`Path: (.*?) \| Keywords: ((?:(?:.*?:){1,2}\d+ ?&?)+)`

This regex will separate out the keywords (last regex's 2nd group) (FOUNDKEYWORD, REGEX (if applicable), LINENUMBER):

`([^:]+?):([^:]+?:)?(\d+)(?: & )?`

## Error

The error file will be formatted like so:

`RELATIVEFILEPATH = ERRORTYPE: ERRORDESCRIPTION`

This regex will separate out the line (RELATIVEFILEPATH, ERRORTYPE, ERRORDESCRIPTION):

`(.*?) = ([\w\.]*?): (.*)`

## Log

The log file is formatted like so:

`DATE TIME MESSAGE`

The message may be formatted like a line in the output file.

# Compiling GoFind

GoFind can be quickly run using Go's `run` command: `go run . directory=../`

To compile GoFind, use Go's native build mechanism: `go build .`

Go uses environment variables to build the application for an operating system, they can be checked with `go env`.

## Windows

In Linux, ensure `GOOS = "windows"`.

Run: `go build -o gofind/gofind.exe .`

## Linux

In Windows, ensure `$env:GOOS = "linux"` (if using PowerShell).

Run: `go build -o gofind/gofind.elf .`

# Developing GoFind

Make changes only to the main branch. The production branch should only contain tested code proven to work.

# ToDo

Most of my ToDo's are listed in the files themselves, but this is a list of some features that may be coming in the future:

- Grab configuration options from a file (most likely JSON).
- Change from using Go's `re` package to something like [hyperscan](https://pkg.go.dev/github.com/flier/gohs/hyperscan)
    - This would give much faster Regex search times and allow for lookahead, a feature Go's `re` package lacks.
- Combine both search types into one function, or take the reused code and put it into a shared function
    - Having two functions only made sense when it was a quick script, there should be no code reuse in the project.

# gomavgen

Generator for standalone Go and C MAVLink libraries. Easy to adapt to other languages.

It reads a MAVLink dialect definition XML file (and recursively its `<include>`s,
to a depth of 5) and renders it through a `text/template` to stdout.

## Usage

    gomavgen <template> path/to/dialect.xml > output

`<template>` is either the name of a builtin template or a path to a template
file on disk. A name is tried first; only if no builtin matches is it treated
as a path.

Builtin templates (embedded in the binary, so no checkout is required):

| name     | output                                  |
|----------|-----------------------------------------|
| `go`     | standalone Go package                   |
| `h`      | C header                                |
| `hh`     | C++ header                              |
| `c_crc`  | C CRC-extra table                       |
| `c_dec`  | C message decoders                      |
| `c_enc`  | C message encoders                      |
| `c_fmt`  | C message formatters                    |

Examples:

    gomavgen go    common.xml > mavlink.go
    gomavgen h     common.xml > mavlink.h
    gomavgen ./my_lang.tmpl common.xml > out   # path fallback

package speech

// libopus links statically, so the box needs no libopus of its own.

// #cgo LDFLAGS: /usr/lib/x86_64-linux-gnu/libopus.a -lm
import "C"

// Package syntaxfixture writes sequence and picture parameter sets and slice
// headers, for tests that need a stream to decode. It reads nothing: the one reader
// of this syntax is the root package, github.com/go-avkit/h264 (ParseSPS, ParsePPS,
// ParseSliceReferences). The types here describe what a test wants written and are
// not what the decoder works on; internal/decoder/compat_test.go turns them into
// the root's.
//
// It was internal/syntax, the decoder's own parser imported from oops1/go.264,
// before the decoder moved to the root's readers.
package syntaxfixture

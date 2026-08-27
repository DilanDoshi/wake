package core

// The model a session runs, and where the list of them comes from.
//
// Not from the wire: the init frame names the model in use and no others. Not
// from `--help`, which gives an e.g. and closes nothing. It comes from **the
// bare `/model` command's own reply**, which prints "Available: sonnet, opus,
// haiku, …, or a full model ID" and costs num_turns=0 and $0 - recorded in
// testdata/stream/bare-model.jsonl and asserted against there, the way the
// palette is asserted against the binary it came out of.
//
// This is where effort and model stop being the same problem all the same.
// EffortLevels is closed and checked; this is a snapshot of one binary, so it
// is offered rather than enforced - see ValidModel. A model shipped tomorrow
// has to be reachable without a Wake release, which is claude's own point when
// it ends the list "or a full model ID".

// ModelDefault is "whatever the operator's own configuration says". A way back
// from a chosen model rather than a model.
const ModelDefault = "default"

// ModelAliases is what a picker offers, in claude's own order, read off the
// recorded reply at 2.1.232. Not a validator - see ValidModel.
var ModelAliases = []string{
	"sonnet", "opus", "haiku", "fable", "best",
	"sonnet[1m]", "opus[1m]", "fable[1m]", "opusplan", ModelDefault,
}

// ValidModel reports whether a name may reach a command line.
//
// Any non-empty name may. The list above is a sample, so a predicate refusing
// what it does not recognise would block every model released after this line
// was written. The empty string means "Wake chose none", which leaves --model
// off the argv entirely - the meaning "" already carries for effort.
func ValidModel(name string) bool { return name != "" }

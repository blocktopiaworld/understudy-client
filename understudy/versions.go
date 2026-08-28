package understudy

// The generated protocol tables register themselves from their init functions,
// so something has to import them. That happens here rather than in the
// command, because every user of this package wants to be able to name a
// version — a Client that cannot resolve "26.1" is not useful, and making each
// caller remember a blank import would turn a build-time mistake into a
// run-time "unsupported version" that only appears at connect time.
//
// The tables live in their own package because they are ~9,700 generated lines
// that would otherwise bury the hand-written wire layer in a directory listing.
// See protocol/doc.go.
import _ "github.com/block-topia/understudy-client/protocol/versions"

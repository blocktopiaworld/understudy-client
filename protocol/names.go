package protocol

import "strings"

// DefaultNamespace is the namespace every vanilla identifier lives in.
const DefaultNamespace = "minecraft"

// DefaultStackSize is what an unrecognised item is assumed to stack to.
const DefaultStackSize int32 = 64

// Namespaced qualifies a bare identifier with the vanilla namespace, leaving
// an already-qualified one alone: "zombie" becomes "minecraft:zombie", while
// "mypack:widget" is returned unchanged.
//
// Every lookup keyed on a wire name goes through here. Callers hand this
// package names typed by a human ("dirt", "diamond_pickaxe"), while the wire
// only ever carries the qualified form — normalising in one place is what
// keeps a bare name from silently matching nothing.
func Namespaced(name string) string {
	if strings.ContainsRune(name, ':') {
		return name
	}
	return DefaultNamespace + ":" + name
}

// BaseID strips a block state or a component list from an identifier:
// "minecraft:wheat[age=7]" and
// "minecraft:potion[potion_contents={potion:\"minecraft:water\"}]" both become
// the id alone.
//
// Commands take those qualifiers and this client does not: it knows an item by
// its id, so a name carrying one can only be answered for the id. Silently
// answering nothing instead is worse — an inventory holding twelve wheat
// reported none of it, because the caller asked for "wheat[age=7]".
//
// Nesting is not a concern. A qualifier is always a suffix, so the first
// bracket ends the id whatever follows it.
func BaseID(name string) string {
	if i := strings.IndexRune(name, '['); i >= 0 {
		return strings.TrimSpace(name[:i])
	}
	return name
}

// Qualifier returns the part BaseID removed, without its brackets, or "" when
// there was none. Reporting it is what keeps the stripping honest: a caller who
// asked for a water bottle and got a count of every potion should be told.
func Qualifier(name string) string {
	i := strings.IndexRune(name, '[')
	if i < 0 || !strings.HasSuffix(name, "]") {
		return ""
	}
	return name[i+1 : len(name)-1]
}

// BareName strips the namespace from an identifier: "minecraft:oak_log"
// becomes "oak_log". An unqualified name is returned unchanged.
func BareName(name string) string {
	if i := strings.IndexRune(name, ':'); i >= 0 {
		return name[i+1:]
	}
	return name
}

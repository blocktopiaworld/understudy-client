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

// BareName strips the namespace from an identifier: "minecraft:oak_log"
// becomes "oak_log". An unqualified name is returned unchanged.
func BareName(name string) string {
	if i := strings.IndexRune(name, ':'); i >= 0 {
		return name[i+1:]
	}
	return name
}

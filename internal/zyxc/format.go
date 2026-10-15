// The file model of the intermediate representation.

package zyxc

// A File is one package's intermediate representation.
type File struct {
	// Target is the target triple the package was checked for.
	Target string
	// Module is the import path of the package.
	Module string
	// Exports is the hash of the package's export interface.
	Exports string
	// Decls holds the top-level nodes: imports first, then the rest in
	// source order.
	Decls []*Node
}

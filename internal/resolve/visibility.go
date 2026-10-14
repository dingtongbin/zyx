// Capitalization-based visibility rules.

package resolve

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
)

// Public reports whether name is visible outside its own package: it must
// start with an uppercase ASCII letter. Every name is judged on its own
// spelling, so a public type can carry private fields and methods.
func Public(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

// checkCrossPackage reports a member that another package cannot name.
// member is the declaration found in the imported interface, pkgName the
// namespace it was reached through.
func (r *resolver) checkCrossPackage(member *Symbol, id *ast.Ident, pkgName string) {
	if member.Public {
		return
	}
	d := diag.New(diag.CodePrivateName, id.Sp,
		"%q is not exported by package %s", id.Name, pkgName)
	if member.Decl != nil {
		d = d.WithNote(member.Decl.Span(), "%q is declared here", id.Name)
	}
	r.diags = append(r.diags, d.WithHelp(
		"export it by starting its name with an uppercase letter, or stop using it across packages"))
}

// checkExportedType reports a private type of this package named from the
// signature of a public declaration, which would leak the type.
func (r *resolver) checkExportedType(sym *Symbol, id *ast.Ident) {
	if !r.exported || sym.Kind != KindType || sym.Public || sym.Owner != nil {
		return
	}
	d := diag.New(diag.CodeExportedPrivateType, id.Sp,
		"public declaration refers to the private type %q", id.Name)
	if sym.Decl != nil {
		d = d.WithNote(sym.Decl.Span(), "%q is declared here", id.Name)
	}
	r.diags = append(r.diags, d.WithHelp(
		"export the type, or use it only in private declarations"))
}

//go:build !windows

package developer

import "context"

// DeskPilot's filesystem boundary relies on native Windows directory pins and
// NT object-manager reparse rejection. Other platforms fail closed rather than
// substituting a pathname check with weaker race guarantees.
type workspace struct{ path, identity string }
type directory struct{ identity string }
type nativeEntry struct {
	name      string
	directory bool
	bytes     int64
	unsafe    bool
}

func openWorkspace(string, bool) (*workspace, error) { return nil, Error("WORKSPACE_UNSUPPORTED") }
func (*workspace) Close()                            {}
func (*directory) Close()                            {}
func (*workspace) parent(string, bool) (*directory, string, error) {
	return nil, "", Error("WORKSPACE_UNSUPPORTED")
}
func (*workspace) snapshot(string) (File, error) { return File{}, Error("WORKSPACE_UNSUPPORTED") }
func (*workspace) snapshotBounded(string, int) (File, error) {
	return File{}, Error("WORKSPACE_UNSUPPORTED")
}
func (*workspace) list(string, int) ([]nativeEntry, bool, error) {
	return nil, false, Error("WORKSPACE_UNSUPPORTED")
}
func (*workspace) commit(context.Context, *directory, string, Change) (ChangeResult, error) {
	return ChangeResult{}, Error("WORKSPACE_UNSUPPORTED")
}

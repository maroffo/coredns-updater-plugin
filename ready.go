// ABOUTME: Readiness reporting for the dynupdate plugin.
// ABOUTME: Satisfies the ready.Readiness interface; returns true once store is loaded.

package dynupdate

// Ready reports whether the plugin is ready to serve DNS queries.
// Once it returns true, CoreDNS will not check again.
func (d *DynUpdate) Ready() bool {
	return d.Store != nil && d.Store.Ready()
}

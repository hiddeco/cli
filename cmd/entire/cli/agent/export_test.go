package agent

// RootedRelativeNameForTesting exposes the rooted-name rule so the Windows
// separator behaviour can be exercised from a Unix test run. See
// rootedRelativeName in session_store.go: Name and ValidateExternalSessionRef
// both refuse under this rule, and it is Windows-only in practice, which is
// otherwise untestable on the platform CI actually runs Go tests on — CI
// cross-compiles for Windows rather than running it.
func RootedRelativeNameForTesting(p string, isSeparator func(byte) bool) bool {
	return rootedRelativeName(p, isSeparator)
}

package docgen

// SetChangelogBounds lowers the bounds of a changelog for a test, so that a cut is shown with kilobytes instead of megabytes; it returns a
// function that restores them.
func SetChangelogBounds(source, body int) func() {
	s, b := changelogSource, changelogBody
	changelogSource, changelogBody = source, body
	return func() { changelogSource, changelogBody = s, b }
}

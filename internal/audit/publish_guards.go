package audit

// ShadowUndetermined is the value of the detail shadowCheck: the first version of the name was published although a remote member of a
// containing virtual repository could not be asked whether it serves the name (repositories-and-proxy.md R-REPO-048).
const ShadowUndetermined = "undetermined"

// PublishGuards adds to the details of a publish's event what the guards of a first publish decided: claim, the outcome of the namespace
// auto claim when there is one (R-REPO-023), and shadowCheck when the shadowing check could not be determined (R-REPO-048). The data ports
// of npm (npm.publish), NuGet and Ansible Galaxy (package.published) write the two keys through it, so a publish records them alike in
// every format that records them.
func PublishGuards(details map[string]any, claim string, shadowUndetermined bool) {
	if claim != "" {
		details["claim"] = claim
	}
	if shadowUndetermined {
		details["shadowCheck"] = ShadowUndetermined
	}
}

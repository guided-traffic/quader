package signing

// The transitions of an identity's state (R-SIG-045, R-SIG-047, R-SIG-048, Q-SIG-145). Every other move is refused; a pending key that
// is discarded leaves the table instead of changing its state.
func transitions() map[[2]string]string {
	return map[[2]string]string{
		{StatePending, StateAnnounced}:  "the CA's certificate for the pending key is uploaded while another identity is current",
		{StatePending, StateCurrent}:    "the CA's certificate for the pending key is uploaded while no identity is current",
		{StateAnnounced, StateCurrent}:  "the switch after the announce period, or at once when no identity is current",
		{StateAnnounced, StatePrevious}: "a newer pair of the files replaces it before it signed, or the values stop naming it",
		{StateCurrent, StatePrevious}:   "the switch to the announced identity, a removal, or the values stop naming the identity of the files",
		{StatePrevious, StateAnnounced}: "the files name it again, or it is the newest upload once the values stop naming an identity",
		{StatePrevious, StateRetired}:   "an administrator retires it",
	}
}

// CanMove reports whether an identity may move from one state to another.
func CanMove(from, to string) bool {
	_, ok := transitions()[[2]string{from, to}]
	return ok
}

// Transitions answers every allowed move with what makes it, for the documentation and the tests.
func Transitions() map[[2]string]string { return transitions() }

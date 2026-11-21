// Package all is the dispatch of the job signature-verify to the verifier of a signature's scheme (signatures.md R-SIG-006, R-SIG-013):
// the bounds that sit outside the libraries (an object above signatures.max_object_size is not read), the time bound of one check, and
// the recovery of a panic in a library, recorded as malformed (content_unreadable).
package all

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/notation"
	"github.com/guided-traffic/quader/internal/signatures/scheme/nugetcms"
	"github.com/guided-traffic/quader/internal/signatures/scheme/openpgp"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey"
	"github.com/guided-traffic/quader/internal/signatures/scheme/sigstore"
)

// verifier is the check of one family of schemes.
type verifier func(ctx context.Context, in scheme.Input) (scheme.Output, error)

// verifierOf is the check of a scheme's family; ok is false for a scheme Quader does not read.
func verifierOf(name string) (verifier, bool) {
	switch name {
	case signatures.SchemeSigstoreBundle, signatures.SchemeCosignLegacy:
		return sigstore.Check, true
	case signatures.SchemeNotationJWS, signatures.SchemeNotationCOSE:
		return notation.Check, true
	case signatures.SchemeNuGetCMS:
		return nugetcms.Check, true
	case signatures.SchemeOpenPGP:
		return openpgp.Check, true
	case signatures.SchemeNPMRegistry, signatures.SchemePublicKey:
		return rawkey.Check, true
	}
	return nil, false
}

// Check reads and checks one signature within timeout (signatures.check_timeout; 0 none). A scheme Quader does not read, and a role of
// attestation without a signature (scheme unknown), is unsupported (scheme_unknown); an object above the bound is malformed
// (limits_exceeded) without a read; a panic of a verifier is malformed (content_unreadable). An error is a check that could not run
// ([scheme.ErrTransient]); a check past its time is one too.
func Check(ctx context.Context, in scheme.Input, timeout time.Duration) (out scheme.Output, err error) {
	if in.Limits == (scheme.Limits{}) {
		in.Limits = scheme.DefaultLimits()
	}
	verify, ok := verifierOf(in.Scheme)
	if !ok {
		return final(in, signatures.StateUnsupported, signatures.ReasonSchemeUnknown), nil
	}
	if in.ObjectSize > in.Limits.MaxObjectSize || int64(len(in.Raw)) > in.Limits.MaxObjectSize {
		return final(in, signatures.StateMalformed, signatures.ReasonLimitsExceeded), nil
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	type answer struct {
		out scheme.Output
		err error
	}
	done := make(chan answer, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- answer{out: final(in, signatures.StateMalformed, signatures.ReasonContentUnreadable)}
			}
		}()
		o, e := verify(ctx, in)
		done <- answer{out: o, err: e}
	}()
	select {
	case a := <-done:
		if a.err != nil && !errors.Is(a.err, scheme.ErrTransient) {
			a.err = fmt.Errorf("%w: %w", scheme.ErrTransient, a.err)
		}
		if a.err == nil {
			a.out = settle(in, a.out)
		}
		return a.out, a.err
	case <-ctx.Done():
		return scheme.Output{}, fmt.Errorf("%w: %w", scheme.ErrTransient, context.Cause(ctx))
	}
}

// final is a result the dispatch decides without a verifier.
func final(in scheme.Input, state signatures.State, reason string) scheme.Output {
	return scheme.Output{Result: signatures.Result{State: state, Reason: reason, Role: in.Role}}
}

// settle fills what a verifier left: the role it did not correct, and a state it did not set.
func settle(in scheme.Input, o scheme.Output) scheme.Output {
	if o.Result.Role == "" || !signatures.RoleValid(o.Result.Role) {
		o.Result.Role = in.Role
	}
	if !o.Result.State.Valid() || o.Result.State == signatures.StatePending {
		o.Result.State, o.Result.Reason = signatures.StateMalformed, signatures.ReasonContentUnreadable
	}
	return o
}

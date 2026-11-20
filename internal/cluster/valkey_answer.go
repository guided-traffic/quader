package cluster

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// The first answer of a replica's Valkey (Q-HA-13, R-HA-008). A replica of the main installation holds its first
// readiness until Valkey has answered once, accepted or refused, or until valkey.startup_answer_timeout has passed
// (the composition root waits on [Valkey.Answered]). What counts as an answer:
//
//   - accepted: an attempt that installed a client. valkey-go's connection opens with HELLO 3 AUTH <user> <password>,
//     so a data node that answered it with its server map has accepted the credential; in Sentinel mode a Sentinel
//     named the primary over an authenticated connection first. The access check below passed or failed for a reason
//     other than a refusal. A server below 9.0 has answered too (the client runs degraded, unsupported).
//   - refused: a configuration error of classifyStartup: WRONGPASS, NOAUTH or NOPERM from a data node or a Sentinel
//     (also NOPERM in the access check), a certificate the replica does not trust or a server that does not speak
//     TLS, every reachable Sentinel answering that it does not know the primary name.
//
// Everything else is silence: a deadline, a refused or reset connection, a name that does not resolve, LOADING or
// MASTERDOWN, no Sentinel reachable, a Sentinel that names a primary that does not answer. Those pass with time (a
// failover, a pod being rescheduled), so they end only at the bound.

// WithScriptCheck makes the first connection also run the take script of the limits (EVAL, then EVALSHA, with no
// key), so that an ACL user without them is refused before the first readiness instead of at the first limited
// request (R-ARCH-031, R-ARCH-034). The main installation's client sets it; the Bastion's user has no scripting
// command and its client does not.
func WithScriptCheck() ValkeyOption { return func(o *valkeyOptions) { o.scriptCheck = true } }

// Answered is closed once Valkey has answered a connection attempt of this client: it accepted the credential, or it
// refused it or the TLS handshake, or every reachable Sentinel said it does not know the primary name. It stays open
// while every attempt meets silence (a deadline, a refused connection, no Sentinel reachable). [Valkey.StartupError]
// tells a refusal from an acceptance. Only the start reads it: a client that has answered once never reopens it.
func (v *Valkey) Answered() <-chan struct{} { return v.answered }

// markAnswered closes Answered once.
func (v *Valkey) markAnswered() { v.answerOnce.Do(func() { close(v.answered) }) }

// checkAccess runs, on the connection of the first successful attempt, what shows that the user may do what Quader
// needs beyond the login: one write of the replica's probe key (SET with its expiry, inside the user's key pattern) and,
// with WithScriptCheck, the take script with no key, as EVAL and as EVALSHA. It returns a refusal (an error of the
// kind auth: NOPERM) and nothing else; any other failure is logged and left to the health probe, which reports it as
// it does today, because the server has answered the login and the attempt is not silence.
func (v *Valkey) checkAccess(c valkey.Client) error {
	ctx, cancel := context.WithTimeout(v.ctx, v.cfg.CommandTimeout)
	defer cancel()
	for _, step := range v.accessSteps(c) {
		err := c.Do(ctx, step.cmd).Error()
		switch kind := classify(err); kind {
		case kindNone:
			continue
		case KindAuth:
			return fmt.Errorf("%s: %w", step.what, err)
		default:
			v.m.errors.WithLabelValues(string(kindLabel(kind))).Inc()
			v.log.Warn("valkey access check did not finish; the health probe reports the state", "step", step.what, "error", err.Error())
			return nil
		}
	}
	return nil
}

type accessStep struct {
	what string
	cmd  valkey.Completed
}

// accessSteps are the commands of checkAccess. Without a keyspace no key can be formed and the write is left out (the
// health probe sends PING then).
func (v *Valkey) accessSteps(c valkey.Client) []accessStep {
	var steps []accessStep
	if key, err := v.formKey(probeFamily, v.instance); err == nil {
		steps = append(steps, accessStep{"the write of the probe key", c.B().Set().Key(key.name).Value("1").Px(key.ttl).Build()})
	}
	if v.scriptCheck {
		// ARGV of the take with no window and no marker: the script reads and writes nothing and answers {1, nil}.
		args := []string{"1", strconv.FormatInt(time.Now().UnixMilli(), 10), "0"}
		steps = append(steps,
			accessStep{"the take script (EVAL)", c.B().Eval().Script(takeScript).Numkeys(0).Arg(args...).Build()},
			accessStep{"the take script (EVALSHA)", c.B().Evalsha().Sha1(takeScriptSHA).Numkeys(0).Arg(args...).Build()},
		)
	}
	return steps
}
